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
	"google.golang.org/grpc/status"

	lerrors "github.com/nisarul/Linea-core/errors"

	"github.com/nisarul/Linea-server/internal/auth"
	"github.com/nisarul/Linea-server/internal/tenancy"
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
			return handler(tenancy.WithTenant(ctx, tenancy.DefaultTenant), req)
		}
		// Auth disabled mode: verifier == nil. Use an anonymous
		// curator so dev/test setups can skip OIDC entirely. This
		// MUST NEVER be used in production; main.go enforces.
		if verifier == nil {
			id := auth.Identity{Subject: "anonymous", Role: auth.RoleCurator}
			ctx = auth.WithIdentity(ctx, id)
			ctx = tenancy.WithTenant(ctx, tenancy.DefaultTenant)
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
		ctx = tenancy.WithTenant(ctx, tenancy.DefaultTenant)
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
			slog.String("tenant", string(tenancy.TenantOf(ctx))),
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
