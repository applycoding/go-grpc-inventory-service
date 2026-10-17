package interceptor

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthUnary requires a static bearer token on every RPC except health and reflection.
func AuthUnary(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorize(ctx, token, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// AuthStream requires a static bearer token on every stream except health and reflection.
func AuthStream(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorize(ss.Context(), token, info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func authorize(ctx context.Context, expected, fullMethod string) error {
	if isPublic(fullMethod) {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing authorization")
	}
	values := md.Get("authorization")
	if len(values) != 1 {
		return status.Error(codes.Unauthenticated, "missing authorization")
	}
	got, ok := bearerToken(values[0])
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid authorization")
	}
	return nil
}

func bearerToken(header string) (string, bool) {
	const scheme = "bearer "
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	return header[len(scheme):], true
}

func isPublic(fullMethod string) bool {
	switch {
	case strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/"):
		return true
	case strings.HasPrefix(fullMethod, "/grpc.reflection.v1.ServerReflection/"):
		return true
	case strings.HasPrefix(fullMethod, "/grpc.reflection.v1alpha.ServerReflection/"):
		return true
	default:
		return false
	}
}
