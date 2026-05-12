// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config carries server configuration sourced from the
// environment (with sensible defaults for dev). Production
// deployments override via env vars or a wrapper.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the resolved server configuration.
type Config struct {
	GRPCAddr   string // e.g. ":9090"
	HTTPAddr   string // e.g. ":8080"
	DataDir    string // path to Badger directory
	AuthMode   string // "oidc" | "disabled" (disabled is dev-only)
	OIDCIssuer string
	OIDCAud    string
	RoleClaim  string

	LogLevel string // "debug" | "info" | "warn" | "error"
}

// FromEnv reads the configuration from environment variables with
// reasonable defaults. Returns an error if a required value is
// missing.
func FromEnv() (Config, error) {
	c := Config{
		GRPCAddr:   getEnv("LINEA_GRPC_ADDR", ":9090"),
		HTTPAddr:   getEnv("LINEA_HTTP_ADDR", ":8080"),
		DataDir:    getEnv("LINEA_DATA_DIR", "./.devdata"),
		AuthMode:   strings.ToLower(getEnv("LINEA_AUTH_MODE", "oidc")),
		OIDCIssuer: os.Getenv("LINEA_OIDC_ISSUER"),
		OIDCAud:    os.Getenv("LINEA_OIDC_AUDIENCE"),
		RoleClaim:  getEnv("LINEA_OIDC_ROLE_CLAIM", "groups"),
		LogLevel:   getEnv("LINEA_LOG_LEVEL", "info"),
	}
	if c.AuthMode != "oidc" && c.AuthMode != "disabled" {
		return c, fmt.Errorf("LINEA_AUTH_MODE must be oidc|disabled, got %q", c.AuthMode)
	}
	if c.AuthMode == "oidc" {
		if c.OIDCIssuer == "" || c.OIDCAud == "" {
			return c, errors.New("LINEA_OIDC_ISSUER and LINEA_OIDC_AUDIENCE are required when LINEA_AUTH_MODE=oidc")
		}
	}
	return c, nil
}

func getEnv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

// AuthDisabled reports whether auth is in dev-only "disabled" mode.
func (c Config) AuthDisabled() bool { return c.AuthMode == "disabled" }

// MustGetInt reads k and parses as int, returning def if absent.
func MustGetInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}
