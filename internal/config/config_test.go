package config

import (
	"testing"
	"time"
)

func TestFromEnvExplicit(t *testing.T) {
	t.Setenv("INVENTORY_GRPC_ADDR", ":19091")
	t.Setenv("INVENTORY_HTTP_ADDR", ":18081")
	t.Setenv("INVENTORY_DATABASE_URL", "postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable")
	t.Setenv("INVENTORY_AUTH_TOKEN", "local-token")
	t.Setenv("INVENTORY_SHUTDOWN_TIMEOUT", "3s")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GRPCAddr != ":19091" || cfg.HTTPAddr != ":18081" || cfg.AuthToken != "local-token" {
		t.Fatalf("cfg %+v", cfg)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("timeout %s", cfg.ShutdownTimeout)
	}
	if cfg.DatabaseURL == "" {
		t.Fatal("expected database url")
	}
}

func TestFromEnvRejectsEmptyToken(t *testing.T) {
	t.Setenv("INVENTORY_AUTH_TOKEN", "   ")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestFromEnvRejectsBadTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "not a duration", value: "soon"},
		{name: "zero", value: "0s"},
		{name: "negative", value: "-1s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("INVENTORY_AUTH_TOKEN", "local-token")
			t.Setenv("INVENTORY_SHUTDOWN_TIMEOUT", tt.value)
			if _, err := FromEnv(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("INVENTORY_DATABASE_URL", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GRPCAddr != defaultGRPCAddr || cfg.HTTPAddr != defaultHTTPAddr || cfg.AuthToken != defaultAuthToken {
		t.Fatalf("cfg %+v", cfg)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("database url %q", cfg.DatabaseURL)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("timeout %s", cfg.ShutdownTimeout)
	}
}
