package grpcserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	inventoryv1 "github.com/arpanbom/go-grpc-inventory-service/gen/inventory/v1"
	"github.com/arpanbom/go-grpc-inventory-service/internal/interceptor"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository/memory"
	"github.com/arpanbom/go-grpc-inventory-service/internal/service"
)

const testToken = "test-token"

func startBufconn(t *testing.T) inventoryv1.InventoryServiceClient {
	t.Helper()
	_, client := startBufconnConn(t)
	return client
}

func startBufconnConn(t *testing.T) (*grpc.ClientConn, inventoryv1.InventoryServiceClient) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.RecoveryUnary(logger),
			interceptor.LoggingUnary(logger),
			interceptor.AuthUnary(testToken),
		),
		grpc.ChainStreamInterceptor(
			interceptor.RecoveryStream(logger),
			interceptor.LoggingStream(logger),
			interceptor.AuthStream(testToken),
		),
	)
	Register(srv, service.New(memory.New()), logger)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, inventoryv1.NewInventoryServiceClient(conn)
}

func authed(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+testToken)
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("code=%s err=%v, want %s", status.Code(err), err, code)
	}
}

func TestBufconnAuthAndHealth(t *testing.T) {
	t.Parallel()
	conn, client := startBufconnConn(t)
	ctx := context.Background()

	_, err := client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{Sku: "MUG-1", Name: "Mug"})
	requireCode(t, err, codes.Unauthenticated)

	bad := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer wrong")
	_, err = client.CreateProduct(bad, &inventoryv1.CreateProductRequest{Sku: "MUG-1", Name: "Mug"})
	requireCode(t, err, codes.Unauthenticated)

	healthClient := healthpb.NewHealthClient(conn)
	resp, err := healthClient.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health status %s", resp.GetStatus())
	}
}

func TestBufconnProductAndReservation(t *testing.T) {
	t.Parallel()
	client := startBufconn(t)
	ctx := authed(context.Background())

	created, err := client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Sku:        "MUG-1",
		Name:       "Stoneware mug",
		PriceCents: 1800,
		OnHand:     5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.GetProduct().GetAvailable() != 5 {
		t.Fatalf("available %d", created.GetProduct().GetAvailable())
	}

	_, err = client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{Sku: "MUG-1", Name: "Other"})
	requireCode(t, err, codes.AlreadyExists)

	got, err := client.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: created.GetProduct().GetId()})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.GetProduct().GetSku() != "MUG-1" {
		t.Fatalf("sku %s", got.GetProduct().GetSku())
	}

	first, err := client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		IdempotencyKey: "order-1",
		ProductId:      created.GetProduct().GetId(),
		Quantity:       2,
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	second, err := client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		IdempotencyKey: "order-1",
		ProductId:      created.GetProduct().GetId(),
		Quantity:       2,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first.GetReservation().GetId() != second.GetReservation().GetId() {
		t.Fatalf("replay id %s, want %s", second.GetReservation().GetId(), first.GetReservation().GetId())
	}
	if first.GetReservation().GetStatus() != inventoryv1.ReservationStatus_RESERVATION_STATUS_HELD {
		t.Fatalf("status %s", first.GetReservation().GetStatus())
	}

	_, err = client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		IdempotencyKey: "too-many",
		ProductId:      created.GetProduct().GetId(),
		Quantity:       20,
	})
	requireCode(t, err, codes.FailedPrecondition)

	committed, err := client.CommitReservation(ctx, &inventoryv1.CommitReservationRequest{Id: first.GetReservation().GetId()})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if committed.GetReservation().GetStatus() != inventoryv1.ReservationStatus_RESERVATION_STATUS_COMMITTED {
		t.Fatalf("status %s", committed.GetReservation().GetStatus())
	}
	_, err = client.ReleaseReservation(ctx, &inventoryv1.ReleaseReservationRequest{Id: first.GetReservation().GetId()})
	requireCode(t, err, codes.FailedPrecondition)

	after, err := client.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: created.GetProduct().GetId()})
	if err != nil {
		t.Fatal(err)
	}
	if after.GetProduct().GetOnHand() != 3 || after.GetProduct().GetReserved() != 0 {
		t.Fatalf("stock on_hand=%d reserved=%d", after.GetProduct().GetOnHand(), after.GetProduct().GetReserved())
	}
}
