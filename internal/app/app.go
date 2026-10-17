// Package app wires the inventory process and shuts it down on context cancel.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"

	"github.com/arpanbom/go-grpc-inventory-service/internal/config"
	"github.com/arpanbom/go-grpc-inventory-service/internal/grpcserver"
	"github.com/arpanbom/go-grpc-inventory-service/internal/httpserver"
	"github.com/arpanbom/go-grpc-inventory-service/internal/interceptor"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository/memory"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository/postgres"
	"github.com/arpanbom/go-grpc-inventory-service/internal/service"
)

// Run listens for gRPC and HTTP until ctx is cancelled or a server fails.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	store, closeStore, err := openStore(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeStore()

	svc := service.New(store)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.RecoveryUnary(logger),
			interceptor.LoggingUnary(logger),
			interceptor.AuthUnary(cfg.AuthToken),
		),
		grpc.ChainStreamInterceptor(
			interceptor.RecoveryStream(logger),
			interceptor.LoggingStream(logger),
			interceptor.AuthStream(cfg.AuthToken),
		),
	)
	healthServer := grpcserver.Register(grpcSrv, svc, logger)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- serveGRPC(grpcSrv, lis)
	}()
	if err := waitForTCP(ctx, lis.Addr().String()); err != nil {
		grpcSrv.Stop()
		return err
	}

	conn, err := grpc.NewClient(
		dialTarget(lis.Addr().String()),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcSrv.Stop()
		return fmt.Errorf("dial local grpc: %w", err)
	}

	handler, err := httpserver.NewHandler(ctx, conn, store)
	if err != nil {
		_ = conn.Close()
		grpcSrv.Stop()
		return fmt.Errorf("http handler: %w", err)
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		errCh <- serveHTTP(httpSrv)
	}()

	logger.Info("listening",
		"grpc", lis.Addr().String(),
		"http", cfg.HTTPAddr,
		"auth", "static-bearer",
	)

	select {
	case <-ctx.Done():
		return shutdown(cfg, logger, healthServer, grpcSrv, httpSrv, conn)
	case err := <-errCh:
		_ = shutdown(cfg, logger, healthServer, grpcSrv, httpSrv, conn)
		if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func openStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (repository.Store, func(), error) {
	if cfg.DatabaseURL == "" {
		logger.Info("store", "driver", "memory")
		return memory.New(), func() {}, nil
	}
	logger.Info("store", "driver", "postgres")
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Close, nil
}

func serveGRPC(server *grpc.Server, lis net.Listener) error {
	err := server.Serve(lis)
	if err == nil || errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return fmt.Errorf("grpc serve: %w", err)
}

func serveHTTP(server *http.Server) error {
	err := server.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("http serve: %w", err)
}

func shutdown(cfg config.Config, logger *slog.Logger, healthServer *health.Server, grpcSrv *grpc.Server, httpSrv *http.Server, conn *grpc.ClientConn) error {
	logger.Info("shutting down")
	healthServer.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	// Close the gateway's loopback client before stopping gRPC. Otherwise the
	// open client connection keeps GracefulStop waiting until the timeout.
	if err := conn.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close grpc client: %w", err))
	}

	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		grpcSrv.Stop()
		errs = append(errs, errors.New("grpc shutdown timed out"))
	}
	return errors.Join(errs...)
}

func dialTarget(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func waitForTCP(ctx context.Context, addr string) error {
	target := dialTarget(addr)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var last error
	for {
		conn, err := net.DialTimeout("tcp", target, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("grpc server %s not ready: %w", target, last)
		case <-ticker.C:
		}
	}
}
