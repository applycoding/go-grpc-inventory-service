// Package httpserver exposes the inventory service over HTTP with gRPC-Gateway.
package httpserver

import (
	"context"
	"io"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	inventoryv1 "github.com/arpanbom/go-grpc-inventory-service/gen/inventory/v1"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
)

// NewHandler returns an HTTP mux for health probes and the REST gateway.
// /healthz and /readyz do not require the bearer token. /v1 routes do.
func NewHandler(ctx context.Context, conn *grpc.ClientConn, store repository.Store) (http.Handler, error) {
	// Authorization is copied into gRPC metadata by the gateway itself.
	gateway := runtime.NewServeMux()
	if err := inventoryv1.RegisterInventoryServiceHandler(ctx, gateway, conn); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			writePlain(w, http.StatusServiceUnavailable, "not ready\n")
			return
		}
		writePlain(w, http.StatusOK, "ok\n")
	})
	mux.Handle("/", gateway)
	return mux, nil
}

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}
