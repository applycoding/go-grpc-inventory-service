package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/arpanbom/go-grpc-inventory-service/internal/config"
)

func TestDialTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{in: ":19090", want: "127.0.0.1:19090"},
		{in: "0.0.0.0:19090", want: "127.0.0.1:19090"},
		{in: "[::]:19090", want: "127.0.0.1:19090"},
		{in: "127.0.0.1:19090", want: "127.0.0.1:19090"},
		{in: "10.0.0.8:19090", want: "10.0.0.8:19090"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := dialTarget(tt.in); got != tt.want {
				t.Fatalf("dialTarget(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	grpcAddr := freeAddr(t)
	httpAddr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, config.Config{
			GRPCAddr:        grpcAddr,
			HTTPAddr:        httpAddr,
			AuthToken:       "test-token",
			ShutdownTimeout: 3 * time.Second,
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("server exited before healthy: %v", err)
		default:
		}
		resp, err := client.Get("http://" + httpAddr + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("server did not become healthy")
		}
		time.Sleep(15 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
