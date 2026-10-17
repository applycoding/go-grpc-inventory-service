// Package config reads process settings from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultGRPCAddr        = ":19090"
	defaultHTTPAddr        = ":18080"
	defaultAuthToken       = "dev-token"
	defaultShutdownTimeout = 10 * time.Second
)

// Config is the process configuration.
type Config struct {
	GRPCAddr        string
	HTTPAddr        string
	DatabaseURL     string
	AuthToken       string
	ShutdownTimeout time.Duration
}

// FromEnv loads configuration. An empty database URL selects the in-memory store.
// The auth token defaults to dev-token for local use and must not be empty when set.
func FromEnv() (Config, error) {
	grpcAddr, err := stringOrDefault("INVENTORY_GRPC_ADDR", defaultGRPCAddr)
	if err != nil {
		return Config{}, err
	}
	httpAddr, err := stringOrDefault("INVENTORY_HTTP_ADDR", defaultHTTPAddr)
	if err != nil {
		return Config{}, err
	}
	token, err := stringOrDefault("INVENTORY_AUTH_TOKEN", defaultAuthToken)
	if err != nil {
		return Config{}, err
	}
	timeout := defaultShutdownTimeout
	if raw, ok := os.LookupEnv("INVENTORY_SHUTDOWN_TIMEOUT"); ok {
		if strings.TrimSpace(raw) == "" {
			return Config{}, fmt.Errorf("INVENTORY_SHUTDOWN_TIMEOUT must not be empty")
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("INVENTORY_SHUTDOWN_TIMEOUT: %w", err)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("INVENTORY_SHUTDOWN_TIMEOUT must be > 0")
		}
		timeout = parsed
	}
	return Config{
		GRPCAddr:        grpcAddr,
		HTTPAddr:        httpAddr,
		DatabaseURL:     strings.TrimSpace(os.Getenv("INVENTORY_DATABASE_URL")),
		AuthToken:       token,
		ShutdownTimeout: timeout,
	}, nil
}

func stringOrDefault(key, fallback string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return value, nil
}
