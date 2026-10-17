package interceptor

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthUnary(t *testing.T) {
	t.Parallel()
	const token = "test-token"
	okHandler := func(context.Context, any) (any, error) { return "ok", nil }
	auth := AuthUnary(token)

	tests := []struct {
		name   string
		ctx    context.Context
		method string
		code   codes.Code
	}{
		{
			name:   "health is public",
			ctx:    context.Background(),
			method: "/grpc.health.v1.Health/Check",
			code:   codes.OK,
		},
		{
			name:   "reflection is public",
			ctx:    context.Background(),
			method: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
			code:   codes.OK,
		},
		{
			name:   "missing metadata",
			ctx:    context.Background(),
			method: "/inventory.v1.InventoryService/CreateProduct",
			code:   codes.Unauthenticated,
		},
		{
			name:   "wrong token",
			ctx:    metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer nope")),
			method: "/inventory.v1.InventoryService/CreateProduct",
			code:   codes.Unauthenticated,
		},
		{
			name:   "bearer token",
			ctx:    metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer test-token")),
			method: "/inventory.v1.InventoryService/CreateProduct",
			code:   codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := auth(tt.ctx, nil, &grpc.UnaryServerInfo{FullMethod: tt.method}, okHandler)
			if status.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v, want %s", status.Code(err), err, tt.code)
			}
		})
	}
}

func TestRecoveryUnary(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	recoverer := RecoveryUnary(logger)
	_, err := recoverer(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/inventory.v1.InventoryService/CreateProduct"}, func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestLoggingUnary(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	loggerInterceptor := LoggingUnary(logger)
	_, err := loggerInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/inventory.v1.InventoryService/GetProduct"}, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.NotFound, "missing")
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code=%s", status.Code(err))
	}
	logged := buf.String()
	if !strings.Contains(logged, "/inventory.v1.InventoryService/GetProduct") || !strings.Contains(logged, "NotFound") {
		t.Fatalf("log = %s", logged)
	}
}
