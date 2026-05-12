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
	"strings"
	"sync/atomic"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/nisarul/Linea-core/store"
	"github.com/nisarul/Linea-core/store/badger"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/config"
	"github.com/nisarul/Linea-server/internal/interceptors"
	"github.com/nisarul/Linea-server/internal/service"
)

// Server bundles the running gRPC + HTTP listeners.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
	store  *badger.Store
	grpc   *grpc.Server
	http   *http.Server
	ready  atomic.Bool
}

// New constructs a Server from cfg. It opens the underlying
// Badger store; the caller MUST call Close.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger, serverVersion string) (*Server, error) {
	st, err := badger.Open(cfg.DataDir, badger.Silent())
	if err != nil {
		return nil, fmt.Errorf("server: open store: %w", err)
	}

	var verifier *auth.Verifier
	if !cfg.AuthDisabled() {
		verifier, err = auth.NewVerifier(ctx, auth.Config{
			IssuerURL: cfg.OIDCIssuer,
			Audience:  cfg.OIDCAud,
			RoleClaim: cfg.RoleClaim,
		})
		if err != nil {
			_ = st.Close()
			return nil, err
		}
	}

	noAuth := map[string]bool{
		"/linea.v1.Server/Version": false, // version still requires auth
	}
	roleMap := buildRoleMap()

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.LoggingInterceptor(logger),
			interceptors.AuthInterceptor(verifier, noAuth),
			interceptors.RoleInterceptor(roleMap),
			interceptors.ErrorInterceptor(),
		),
	)
	registerServices(grpcSrv, st, serverVersion)

	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			// Pass through Authorization so the AuthInterceptor sees it.
			if strings.EqualFold(key, "authorization") {
				return key, true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
	)
	if err := registerHandlers(ctx, mux, cfg.GRPCAddr); err != nil {
		_ = st.Close()
		return nil, err
	}
	httpHandler := withHealth(mux, &Server{}) // placeholder, set after construct

	srv := &Server{
		cfg:    cfg,
		logger: logger,
		store:  st,
		grpc:   grpcSrv,
		http: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           httpHandler,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	// Replace placeholder with real probes wired to srv.
	srv.http.Handler = withHealth(mux, srv)
	return srv, nil
}

// Store returns the underlying store, primarily for tests.
func (s *Server) Store() store.Store { return s.store }

// Close releases all resources.
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
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Run starts both listeners and blocks until ctx is cancelled.
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

func registerServices(g *grpc.Server, st store.Store, serverVersion string) {
	pb.RegisterServerServer(g, &service.ServerService{Store: st, ServerVersion: serverVersion})
	pb.RegisterPersonsServer(g, &service.PersonsService{Store: st})
	pb.RegisterRelationshipsServer(g, &service.RelationshipsService{Store: st})
	pb.RegisterSourcesServer(g, &service.SourcesService{Store: st})
	pb.RegisterProposalsServer(g, &service.ProposalsService{Store: st})
	pb.RegisterQueriesServer(g, &service.QueriesService{Store: st})
}

// registerHandlers wires the grpc-gateway REST mux to dial back
// into the local gRPC server. Inter-process loopback dial keeps
// the two surfaces share the same interceptor chain (auth, role,
// logging) without duplicating handler code.
func registerHandlers(ctx context.Context, mux *runtime.ServeMux, grpcAddr string) error {
	dial := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	endpoint := grpcAddr
	if strings.HasPrefix(endpoint, ":") {
		endpoint = "127.0.0.1" + endpoint
	}
	if err := pb.RegisterServerHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Server: %w", err)
	}
	if err := pb.RegisterPersonsHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Persons: %w", err)
	}
	if err := pb.RegisterRelationshipsHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Relationships: %w", err)
	}
	if err := pb.RegisterSourcesHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Sources: %w", err)
	}
	if err := pb.RegisterProposalsHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Proposals: %w", err)
	}
	if err := pb.RegisterQueriesHandlerFromEndpoint(ctx, mux, endpoint, dial); err != nil {
		return fmt.Errorf("gateway: register Queries: %w", err)
	}
	return nil
}

// buildRoleMap declares the minimum role required per RPC.
//
// CCGGS §8.1:
//
//	Viewer       — Get*, List*, Queries.*, Server.*
//	Contributor  — + Proposals.CreateProposal, Submit, Withdraw
//	Curator      — + Proposals.Claim, Accept, Reject
func buildRoleMap() map[string]auth.Role {
	const (
		serverPfx       = "/linea.v1.Server/"
		personsPfx      = "/linea.v1.Persons/"
		relationshipsP  = "/linea.v1.Relationships/"
		sourcesPfx      = "/linea.v1.Sources/"
		queriesPfx      = "/linea.v1.Queries/"
		proposalsPfx    = "/linea.v1.Proposals/"
	)
	m := map[string]auth.Role{
		// Viewer reads
		serverPfx + "Version":             auth.RoleViewer,
		personsPfx + "GetPerson":          auth.RoleViewer,
		personsPfx + "ListPersons":        auth.RoleViewer,
		relationshipsP + "GetRelationship":   auth.RoleViewer,
		relationshipsP + "ListRelationships": auth.RoleViewer,
		sourcesPfx + "GetSource":          auth.RoleViewer,
		sourcesPfx + "ListSources":        auth.RoleViewer,
		queriesPfx + "FindPaths":          auth.RoleViewer,
		queriesPfx + "NKCA":               auth.RoleViewer,
		proposalsPfx + "GetProposal":      auth.RoleViewer,
		proposalsPfx + "ListProposals":    auth.RoleViewer,
		// Contributor writes
		proposalsPfx + "CreateProposal":   auth.RoleContributor,
		proposalsPfx + "Submit":           auth.RoleContributor,
		proposalsPfx + "Withdraw":         auth.RoleContributor,
		// Curator decisions
		proposalsPfx + "Claim":            auth.RoleCurator,
		proposalsPfx + "Accept":           auth.RoleCurator,
		proposalsPfx + "Reject":           auth.RoleCurator,
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
