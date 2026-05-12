// SPDX-License-Identifier: AGPL-3.0-or-later

// Package interceptors provides gRPC unary interceptors for
// authentication, structured logging, role enforcement, and the
// translation from Linea-core typed errors to gRPC status codes.
package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	lerrors "github.com/nisarul/Linea-core/errors"

	"github.com/nisarul/Linea-server/internal/auth"
)

// AuthInterceptor extracts the bearer token from incoming
// gRPC metadata, verifies it, and attaches the resulting
// auth.Identity (and v0.1's constant tenant) to the context.
//
// Methods listed in noAuth (e.g. health probes, server metadata)
// are passed through unauthenticated.
func AuthInterceptor(verifier *auth.Verifier, noAuth map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		if noAuth[info.FullMethod] {
			return handler(ctx, req)
		}
		// Auth disabled mode: verifier == nil. Use an anonymous
		// curator so dev/test setups can skip OIDC entirely. This
		// MUST NEVER be used in production; main.go enforces.
		if verifier == nil {
			id := auth.Identity{Subject: "anonymous", Role: auth.RoleCurator}
			ctx = auth.WithIdentity(ctx, id)
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		token := bearerToken(md)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		id, err := verifier.VerifyToken(ctx, token)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}
		if id.Role == auth.RoleNone {
			return nil, status.Error(codes.PermissionDenied, "no Linea role granted")
		}
		ctx = auth.WithIdentity(ctx, id)
		return handler(ctx, req)
	}
}

func bearerToken(md metadata.MD) string {
	for _, v := range md.Get("authorization") {
		const pfx = "Bearer "
		if strings.HasPrefix(v, pfx) {
			return strings.TrimSpace(v[len(pfx):])
		}
	}
	return ""
}

// RoleInterceptor enforces minimum-required roles per RPC method.
// roles maps the gRPC FullMethod string ("/linea.v1.Persons/GetPerson")
// to the minimum auth.Role required to invoke it. Methods absent
// from the map require RoleNone and pass through. The AuthInterceptor
// MUST run before this one so an Identity is in context.
func RoleInterceptor(roles map[string]auth.Role) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		min, ok := roles[info.FullMethod]
		if !ok {
			return handler(ctx, req)
		}
		id := auth.IdentityOf(ctx)
		if id.Role < min {
			return nil, status.Errorf(codes.PermissionDenied,
				"role %s required, have %s", min, id.Role)
		}
		return handler(ctx, req)
	}
}

// LoggingInterceptor emits one structured log line per RPC.
func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		id := auth.IdentityOf(ctx)
		attrs := []slog.Attr{
			slog.String("method", info.FullMethod),
			slog.String("subject", id.Subject),
			slog.String("role", id.Role.String()),
			slog.Duration("dur", time.Since(start)),
		}
		if err != nil {
			st, _ := status.FromError(err)
			attrs = append(attrs, slog.String("code", st.Code().String()))
			logger.LogAttrs(ctx, slog.LevelWarn, "rpc", attrs...)
		} else {
			logger.LogAttrs(ctx, slog.LevelInfo, "rpc", attrs...)
		}
		return resp, err
	}
}

// RateLimitInterceptor rejects calls when the per-key rate
// budget is exhausted. The keyFn picks the bucket key; typical
// choices are "<subject>" for authenticated requests or the
// peer address for anonymous ones.
//
// Returns codes.ResourceExhausted on rejection so clients can
// distinguish from auth failures.
func RateLimitInterceptor(limiter rateLimiter, keyFn func(context.Context) string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		key := keyFn(ctx)
		if key == "" {
			return handler(ctx, req)
		}
		if !limiter.Allow(key) {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
		return handler(ctx, req)
	}
}

// rateLimiter is the minimal interface RateLimitInterceptor uses.
// It matches *ratelimit.Bucket and ratelimit.AlwaysAllow without
// pulling that package into the import cycle.
type rateLimiter interface {
	Allow(key string) bool
}

// SubjectOrPeerKey returns the JWT subject if authenticated,
// else the peer address (best-effort). Used as the default
// keyFn for RateLimitInterceptor.
func SubjectOrPeerKey(ctx context.Context) string {
	if id := auth.IdentityOf(ctx); id.Subject != "" {
		return "u:" + id.Subject
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return "ip:" + p.Addr.String()
	}
	return ""
}

// ErrorInterceptor maps Linea-core typed errors to gRPC status codes.
// Without this, every Linea error would be reported as Unknown.
func ErrorInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		var le *lerrors.Error
		if !errors.As(err, &le) {
			return resp, err
		}
		// If a service already wrapped this with a gRPC status,
		// don't double-wrap.
		if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
			return resp, err
		}
		return nil, status.Error(codeFor(le.Code), le.Error())
	}
}

func codeFor(c lerrors.Code) codes.Code {
	switch c {
	case lerrors.CodeNoKnownConnection:
		return codes.NotFound
	case lerrors.CodePersonNotFound, lerrors.CodeRelationshipNotFound,
		lerrors.CodeSourceNotFound, lerrors.CodeProposalNotFound,
		lerrors.CodeVersionNotFound:
		return codes.NotFound
	case lerrors.CodeInvalidArgument, lerrors.CodeForbiddenRelationship:
		return codes.InvalidArgument
	case lerrors.CodeInvalidTransition, lerrors.CodeImmutableTerminalProposal,
		lerrors.CodeCycleDetected, lerrors.CodeFabricationAttempt:
		return codes.FailedPrecondition
	}
	return codes.Unknown
}
