package interceptor

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// LoggingUnary records method, status code, and duration for each RPC.
func LoggingUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logCall(logger, info.FullMethod, err, start)
		return resp, err
	}
}

// LoggingStream records method, status code, and duration for each stream.
func LoggingStream(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		logCall(logger, info.FullMethod, err, start)
		return err
	}
}

func logCall(logger *slog.Logger, method string, err error, start time.Time) {
	logger.Info("grpc",
		"method", method,
		"code", status.Code(err).String(),
		"duration_ms", time.Since(start).Milliseconds(),
	)
}
