package interceptor

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryUnary turns panics into Internal errors.
func RecoveryUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logPanic(logger, info.FullMethod, recovered)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStream turns panics into Internal errors.
func RecoveryStream(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logPanic(logger, info.FullMethod, recovered)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

func logPanic(logger *slog.Logger, method string, recovered any) {
	logger.Error("grpc panic",
		"method", method,
		"panic", fmt.Sprint(recovered),
		"stack", string(debug.Stack()),
	)
}
