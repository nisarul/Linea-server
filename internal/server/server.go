// SPDX-License-Identifier: AGPL-3.0-or-later

// Package server wires together the gRPC services, the
// grpc-gateway REST surface, the auth/role/log/error
// interceptors, and the health endpoints.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/authz"
	"github.com/nisarul/Linea-server/internal/config"
	"github.com/nisarul/Linea-server/internal/interceptors"
	"github.com/nisarul/Linea-server/internal/platform"
	"github.com/nisarul/Linea-server/internal/quotas"
	"github.com/nisarul/Linea-server/internal/ratelimit"
	"github.com/nisarul/Linea-server/internal/service"
	"github.com/nisarul/Linea-server/internal/tenants"
)

// Server bundles the running gRPC + HTTP listeners.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	platform *platform.Store
	tenants  *tenants.Manager
	grpc     *grpc.Server
	http     *http.Server
	ready    atomic.Bool
}

// New constructs a Server from cfg. It opens the platform store
// and the per-genealogy TenantManager; the caller MUST call Close.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger, serverVersion string) (*Server, error) {
	platformDir := filepath.Join(cfg.DataDir, "platform")
	tenantsDir := filepath.Join(cfg.DataDir, "genealogies")

	pStore, err := platform.Open(platformDir)
	if err != nil {
		return nil, fmt.Errorf("server: open platform: %w", err)
	}
	tMgr, err := tenants.New(tenants.Config{
		Root:    tenantsDir,
		MaxOpen: 64,
	})
	if err != nil {
		_ = pStore.Close()
		return nil, err
	}

	var verifier *auth.Verifier
	if !cfg.AuthDisabled() {
		verifier, err = auth.NewVerifier(ctx, auth.Config{
			IssuerURL: cfg.OIDCIssuer,
			Audience:  cfg.OIDCAud,
			RoleClaim: cfg.RoleClaim,
		})
		if err != nil {
			_ = tMgr.Close()
			_ = pStore.Close()
			return nil, err
		}
	}

	authzResolver := authz.NewResolver(pStore, 5*time.Second)
	enforcer := quotas.New(pStore, quotas.Default())

	// In-process rate limiter. Per-key bucket: 60 burst, 60/sec
	// refill (so authenticated users get one steady RPS plus a
	// burst; anonymous IPs are limited to the same envelope).
	// v0.3 swaps in a Redis-backed limiter so the limit becomes
	// global across replicas.
	rl := ratelimit.New(ratelimit.Config{
		Capacity:        60,
		RefillPerSecond: 60,
	})

	deps := &service.PlatformDeps{
		Platform: pStore,
		Authz:    authzResolver,
		Tenants:  tMgr,
		Quotas:   enforcer,
	}

	noAuth := map[string]bool{
		// Server.Version is callable without auth so health
		// checks and version probes don't need a token.
		"/linea.v1.Server/Version": true,
	}
	roleMap := buildLegacyRoleMap()

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.LoggingInterceptor(logger),
			interceptors.AuthInterceptor(verifier, noAuth),
			interceptors.RateLimitInterceptor(rl, interceptors.SubjectOrPeerKey),
			interceptors.RoleInterceptor(roleMap),
			interceptors.ErrorInterceptor(),
		),
	)
	registerServices(grpcSrv, deps, serverVersion)

	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			if strings.EqualFold(key, "authorization") {
				return key, true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
	)
	if err := registerHandlers(ctx, mux, cfg.GRPCAddr); err != nil {
		_ = tMgr.Close()
		_ = pStore.Close()
		return nil, err
	}

	srv := &Server{
		cfg:      cfg,
		logger:   logger,
		platform: pStore,
		tenants:  tMgr,
		grpc:     grpcSrv,
		http: &http.Server{
			Addr:              cfg.HTTPAddr,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	srv.http.Handler = withHealth(mux, srv)
	return srv, nil
}

// Platform exposes the platform store, primarily for tests.
func (s *Server) Platform() *platform.Store { return s.platform }

// Tenants exposes the tenant manager, primarily for tests.
func (s *Server) Tenants() *tenants.Manager { return s.tenants }

func (s *Server) Close() error {
	var errs []error
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
	if s.http != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	if s.tenants != nil {
		if err := s.tenants.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.platform != nil {
		if err := s.platform.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Server) Run(ctx context.Context) error {
	grpcLn, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("server: listen grpc: %w", err)
	}
	errCh := make(chan error, 2)

	go func() {
		s.logger.Info("grpc listening", slog.String("addr", s.cfg.GRPCAddr))
		errCh <- s.grpc.Serve(grpcLn)
	}()
	go func() {
		s.logger.Info("http listening", slog.String("addr", s.cfg.HTTPAddr))
		s.ready.Store(true)
		err := s.http.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		return s.Close()
	case err := <-errCh:
		_ = s.Close()
		return err
	}
}

func registerServices(g *grpc.Server, deps *service.PlatformDeps, serverVersion string) {
	pb.RegisterServerServer(g, &service.ServerService{ServerVersion: serverVersion})
	pb.RegisterPersonsServer(g, service.NewPersonsService(deps))
	pb.RegisterRelationshipsServer(g, service.NewRelationshipsService(deps))
	pb.RegisterSourcesServer(g, service.NewSourcesService(deps))
	pb.RegisterProposalsServer(g, service.NewProposalsService(deps))
	pb.RegisterQueriesServer(g, service.NewQueriesService(deps))
	pb.RegisterGenealogiesServer(g, service.NewGenealogiesService(deps))
}

func registerHandlers(ctx context.Context, mux *runtime.ServeMux, grpcAddr string) error {
	dial := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	endpoint := grpcAddr
	if strings.HasPrefix(endpoint, ":") {
		endpoint = "127.0.0.1" + endpoint
	}
	regs := []func(context.Context, *runtime.ServeMux, string, []grpc.DialOption) error{
		pb.RegisterServerHandlerFromEndpoint,
		pb.RegisterPersonsHandlerFromEndpoint,
		pb.RegisterRelationshipsHandlerFromEndpoint,
		pb.RegisterSourcesHandlerFromEndpoint,
		pb.RegisterProposalsHandlerFromEndpoint,
		pb.RegisterQueriesHandlerFromEndpoint,
		pb.RegisterGenealogiesHandlerFromEndpoint,
	}
	for _, r := range regs {
		if err := r(ctx, mux, endpoint, dial); err != nil {
			return fmt.Errorf("gateway: register: %w", err)
		}
	}
	return nil
}

// buildLegacyRoleMap restricts the broad RPCs by required global
// "Linea role" mapped from the JWT (Viewer/Contributor/Curator).
//
// Per-genealogy Owner/Curator/Contributor/Viewer enforcement
// happens INSIDE each handler via authz.Resolver — this is only
// the coarse "is this user authenticated and not zero-roled" gate.
func buildLegacyRoleMap() map[string]auth.Role {
	const (
		personsPfx      = "/linea.v1.Persons/"
		relationshipsPx = "/linea.v1.Relationships/"
		sourcesPfx      = "/linea.v1.Sources/"
		queriesPfx      = "/linea.v1.Queries/"
		proposalsPfx    = "/linea.v1.Proposals/"
		genealogiesPfx  = "/linea.v1.Genealogies/"
	)
	// All read RPCs require at least Viewer; mutation RPCs require
	// Contributor; the genealogy CRUD service also requires
	// Contributor at the global level (per-genealogy enforcement
	// is the real gate).
	//
	// Server.Version is intentionally absent from this map: it is
	// also in the noAuth set, so no Identity is present at this
	// point and the role interceptor passes the call through.
	m := map[string]auth.Role{
		personsPfx + "GetPerson":             auth.RoleViewer,
		personsPfx + "ListPersons":           auth.RoleViewer,
		relationshipsPx + "GetRelationship":   auth.RoleViewer,
		relationshipsPx + "ListRelationships": auth.RoleViewer,
		sourcesPfx + "GetSource":             auth.RoleViewer,
		sourcesPfx + "ListSources":           auth.RoleViewer,
		queriesPfx + "FindPaths":             auth.RoleViewer,
		queriesPfx + "NKCA":                  auth.RoleViewer,
		proposalsPfx + "GetProposal":         auth.RoleViewer,
		proposalsPfx + "ListProposals":       auth.RoleViewer,
		proposalsPfx + "CreateProposal":      auth.RoleContributor,
		proposalsPfx + "Submit":              auth.RoleContributor,
		proposalsPfx + "Withdraw":            auth.RoleContributor,
		proposalsPfx + "Claim":               auth.RoleContributor,
		proposalsPfx + "Accept":              auth.RoleContributor,
		proposalsPfx + "Reject":              auth.RoleContributor,
		genealogiesPfx + "ListGenealogies":   auth.RoleViewer,
		genealogiesPfx + "GetGenealogy":      auth.RoleViewer,
		genealogiesPfx + "ListMembers":       auth.RoleViewer,
		genealogiesPfx + "CreateGenealogy":   auth.RoleContributor,
		genealogiesPfx + "UpdateVisibility":  auth.RoleContributor,
		genealogiesPfx + "DeleteGenealogy":   auth.RoleContributor,
		genealogiesPfx + "UpsertMembership":  auth.RoleContributor,
		genealogiesPfx + "RemoveMember":      auth.RoleContributor,
		genealogiesPfx + "LeaveGenealogy":    auth.RoleContributor,
		genealogiesPfx + "BanUser":           auth.RoleContributor,
		genealogiesPfx + "UnbanUser":         auth.RoleContributor,
	}
	return m
}

// withHealth wraps the gateway mux with /healthz and /readyz.
func withHealth(mux *runtime.ServeMux, srv *Server) http.Handler {
	h := http.NewServeMux()
	h.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if srv != nil && srv.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	h.Handle("/", mux)
	return h
}
