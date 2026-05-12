// SPDX-License-Identifier: AGPL-3.0-or-later

// Command lineasrv is the Linea gRPC + REST server.
//
// Configuration is read from environment variables (see
// internal/config). Auth defaults to OIDC; LINEA_AUTH_MODE=disabled
// is provided for local dev only and is refused if LINEA_ENV=production.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nisarul/Linea-server/internal/config"
	"github.com/nisarul/Linea-server/internal/server"
)

// version is overridden at build time via -ldflags.
var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if cfg.AuthDisabled() && strings.EqualFold(os.Getenv("LINEA_ENV"), "production") {
		return fmt.Errorf("LINEA_AUTH_MODE=disabled is forbidden when LINEA_ENV=production")
	}

	logger := newLogger(cfg.LogLevel)
	logger.Info("starting lineasrv",
		slog.String("version", version),
		slog.String("data_dir", cfg.DataDir),
		slog.String("auth_mode", cfg.AuthMode),
		slog.String("grpc_addr", cfg.GRPCAddr),
		slog.String("http_addr", cfg.HTTPAddr),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv, err := server.New(ctx, cfg, logger, version)
	if err != nil {
		return err
	}
	defer srv.Close()

	return srv.Run(ctx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
