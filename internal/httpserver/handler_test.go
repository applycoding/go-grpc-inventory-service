package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"

	inventoryv1 "github.com/arpanbom/go-grpc-inventory-service/gen/inventory/v1"
	"github.com/arpanbom/go-grpc-inventory-service/internal/grpcserver"
	"github.com/arpanbom/go-grpc-inventory-service/internal/interceptor"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository/memory"
	"github.com/arpanbom/go-grpc-inventory-service/internal/service"
)

const testToken = "test-token"

type downStore struct {
	repository.Store
}

func (downStore) Ping(context.Context) error {
	return errors.New("down")
}

func newAPI(t *testing.T, store repository.Store, ready repository.Store) http.Handler {
	t.Helper()
	if store == nil {
		store = memory.New()
	}
	if ready == nil {
		ready = store
	}
	lis := bufconn.Listen(1 << 20)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.RecoveryUnary(logger),
			interceptor.LoggingUnary(logger),
			interceptor.AuthUnary(testToken),
		),
	)
	grpcserver.Register(srv, service.New(store), logger)
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

	handler, err := NewHandler(context.Background(), conn, ready)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

func perform(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestHealthProbes(t *testing.T) {
	t.Parallel()
	store := memory.New()
	handler := newAPI(t, store, nil)

	health := perform(handler, http.MethodGet, "/healthz", "", "")
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("healthz %d %q", health.Code, health.Body.String())
	}
	ready := perform(handler, http.MethodGet, "/readyz", "", "")
	if ready.Code != http.StatusOK {
		t.Fatalf("readyz %d %q", ready.Code, ready.Body.String())
	}

	down := newAPI(t, store, downStore{Store: store})
	notReady := perform(down, http.MethodGet, "/readyz", "", "")
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz down %d", notReady.Code)
	}
}

func TestRESTProductReservation(t *testing.T) {
	t.Parallel()
	handler := newAPI(t, nil, nil)

	missing := perform(handler, http.MethodPost, "/v1/products", `{"sku":"MUG-9","name":"Mug","priceCents":1800,"onHand":4}`, "")
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth %d %s", missing.Code, missing.Body.String())
	}

	createdRec := perform(handler, http.MethodPost, "/v1/products", `{"sku":"MUG-9","name":"Mug","priceCents":1800,"onHand":4}`, testToken)
	if createdRec.Code != http.StatusOK {
		t.Fatalf("create %d %s", createdRec.Code, createdRec.Body.String())
	}
	var created inventoryv1.CreateProductResponse
	if err := protojson.Unmarshal(createdRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v body %s", err, createdRec.Body.String())
	}
	productID := created.GetProduct().GetId()
	if created.GetProduct().GetSku() != "MUG-9" || created.GetProduct().GetAvailable() != 4 {
		t.Fatalf("product %+v", created.GetProduct())
	}

	adjust := perform(handler, http.MethodPost, "/v1/products/"+productID+":adjustStock", `{"delta":1}`, testToken)
	if adjust.Code != http.StatusOK {
		t.Fatalf("adjust %d %s", adjust.Code, adjust.Body.String())
	}

	reservedRec := perform(handler, http.MethodPost, "/v1/reservations", `{"idempotencyKey":"order-9","productId":"`+productID+`","quantity":2}`, testToken)
	if reservedRec.Code != http.StatusOK {
		t.Fatalf("reserve %d %s", reservedRec.Code, reservedRec.Body.String())
	}
	var reserved inventoryv1.ReserveStockResponse
	if err := protojson.Unmarshal(reservedRec.Body.Bytes(), &reserved); err != nil {
		t.Fatalf("decode reserve: %v", err)
	}
	replay := perform(handler, http.MethodPost, "/v1/reservations", `{"idempotencyKey":"order-9","productId":"`+productID+`","quantity":2}`, testToken)
	var replayed inventoryv1.ReserveStockResponse
	if err := protojson.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay: %v body %s", err, replay.Body.String())
	}
	if replayed.GetReservation().GetId() != reserved.GetReservation().GetId() {
		t.Fatalf("replay id %s", replayed.GetReservation().GetId())
	}

	second := perform(handler, http.MethodPost, "/v1/products", `{"sku":"MUG-10","name":"Other mug","priceCents":900,"onHand":1}`, testToken)
	if second.Code != http.StatusOK {
		t.Fatalf("second create %d %s", second.Code, second.Body.String())
	}

	listed := perform(handler, http.MethodGet, "/v1/products?pageSize=1", "", testToken)
	if listed.Code != http.StatusOK {
		t.Fatalf("list %d %s", listed.Code, listed.Body.String())
	}
	var page inventoryv1.ListProductsResponse
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v body %s", err, listed.Body.String())
	}
	if len(page.GetProducts()) != 1 || page.GetNextPageToken() != "1" {
		t.Fatalf("products=%d token=%q", len(page.GetProducts()), page.GetNextPageToken())
	}

	released := perform(handler, http.MethodPost, "/v1/reservations/"+reserved.GetReservation().GetId()+":release", `{}`, testToken)
	if released.Code != http.StatusOK {
		t.Fatalf("release %d %s", released.Code, released.Body.String())
	}
}
