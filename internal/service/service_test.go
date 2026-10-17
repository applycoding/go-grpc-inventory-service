package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository/memory"
)

func newSvc() *Service {
	return New(memory.New())
}

func mustProduct(t *testing.T, svc *Service, sku string, onHand int32) domain.Product {
	t.Helper()
	product, err := svc.CreateProduct(context.Background(), CreateProductInput{
		SKU:        sku,
		Name:       sku + " name",
		PriceCents: 1250,
		OnHand:     onHand,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	return product
}

func TestCreateProductValidation(t *testing.T) {
	t.Parallel()
	svc := newSvc()

	tests := []struct {
		name string
		in   CreateProductInput
	}{
		{name: "empty sku", in: CreateProductInput{Name: "Mug", PriceCents: 1, OnHand: 1}},
		{name: "bad sku", in: CreateProductInput{SKU: "has space", Name: "Mug", PriceCents: 1}},
		{name: "empty name", in: CreateProductInput{SKU: "MUG-1", PriceCents: 1}},
		{name: "negative price", in: CreateProductInput{SKU: "MUG-1", Name: "Mug", PriceCents: -1}},
		{name: "negative stock", in: CreateProductInput{SKU: "MUG-1", Name: "Mug", OnHand: -1}},
		{name: "long description", in: CreateProductInput{SKU: "MUG-1", Name: "Mug", Description: string(make([]byte, 2001))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.CreateProduct(context.Background(), tt.in)
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("error = %v, want invalid argument", err)
			}
		})
	}
}

func TestCreateAndGetProduct(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()

	created := mustProduct(t, svc, "MUG-001", 8)
	if created.Available() != 8 || created.Reserved != 0 {
		t.Fatalf("stock = on_hand %d reserved %d", created.OnHand, created.Reserved)
	}

	got, err := svc.GetProduct(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SKU != "MUG-001" || got.PriceCents != 1250 {
		t.Fatalf("got %+v", got)
	}

	_, err = svc.GetProduct(ctx, "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	_, err = svc.CreateProduct(ctx, CreateProductInput{SKU: "MUG-001", Name: "Other", PriceCents: 1})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestListProductsPagination(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()
	first := mustProduct(t, svc, "A-1", 1)
	second := mustProduct(t, svc, "A-2", 1)
	third := mustProduct(t, svc, "A-3", 1)

	page, token, err := svc.ListProducts(ctx, 2, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page) != 2 || token == "" {
		t.Fatalf("page=%d token=%q", len(page), token)
	}
	if page[0].ID != first.ID || page[1].ID != second.ID {
		t.Fatalf("order = %s %s", page[0].SKU, page[1].SKU)
	}

	rest, next, err := svc.ListProducts(ctx, 2, token)
	if err != nil {
		t.Fatalf("list next: %v", err)
	}
	if len(rest) != 1 || rest[0].ID != third.ID || next != "" {
		t.Fatalf("rest=%d next=%q", len(rest), next)
	}

	if _, _, err := svc.ListProducts(ctx, 1, "nope"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("bad token error = %v", err)
	}
	if _, _, err := svc.ListProducts(ctx, 101, ""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("big page error = %v", err)
	}
}

func TestAdjustStock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		onHand     int32
		reserve    int32
		delta      int32
		wantOnHand int32
		wantErr    error
	}{
		{name: "add", onHand: 5, delta: 2, wantOnHand: 7},
		{name: "down to reserved", onHand: 10, reserve: 4, delta: -6, wantOnHand: 4},
		{name: "below reserved", onHand: 10, reserve: 4, delta: -7, wantErr: domain.ErrInsufficientStock},
		{name: "below zero", onHand: 2, delta: -3, wantErr: domain.ErrInsufficientStock},
		{name: "zero delta", onHand: 2, delta: 0, wantErr: domain.ErrInvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newSvc()
			ctx := context.Background()
			product := mustProduct(t, svc, "SKU-1", tt.onHand)
			if tt.reserve > 0 {
				if _, err := svc.ReserveStock(ctx, "hold-1", product.ID, tt.reserve); err != nil {
					t.Fatalf("reserve: %v", err)
				}
			}
			updated, err := svc.AdjustStock(ctx, product.ID, tt.delta)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("adjust: %v", err)
			}
			if updated.OnHand != tt.wantOnHand || updated.Reserved != tt.reserve {
				t.Fatalf("on_hand=%d reserved=%d", updated.OnHand, updated.Reserved)
			}
		})
	}
}

func TestAdjustMissingProduct(t *testing.T) {
	t.Parallel()
	_, err := newSvc().AdjustStock(context.Background(), "missing", 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestReserveStock(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()
	product := mustProduct(t, svc, "BOLT-1", 5)

	tests := []struct {
		name    string
		key     string
		product string
		qty     int32
		wantErr error
	}{
		{name: "missing key", product: product.ID, qty: 1, wantErr: domain.ErrInvalidArgument},
		{name: "missing product id", key: "k1", qty: 1, wantErr: domain.ErrInvalidArgument},
		{name: "zero qty", key: "k1", product: product.ID, qty: 0, wantErr: domain.ErrInvalidArgument},
		{name: "unknown product", key: "k1", product: "nope", qty: 1, wantErr: domain.ErrNotFound},
		{name: "too many", key: "k1", product: product.ID, qty: 6, wantErr: domain.ErrInsufficientStock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ReserveStock(ctx, tt.key, tt.product, tt.qty)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	got, err := svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reserved != 0 || got.OnHand != 5 {
		t.Fatalf("stock mutated: %+v", got)
	}
}

func TestReserveIdempotency(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()
	product := mustProduct(t, svc, "BOLT-2", 10)

	first, err := svc.ReserveStock(ctx, "order-9", product.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ReserveStock(ctx, "order-9", product.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Status != domain.ReservationStatusHeld {
		t.Fatalf("replay = %+v", second)
	}
	got, err := svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reserved != 4 || got.Available() != 6 {
		t.Fatalf("stock = %+v", got)
	}

	_, err = svc.ReserveStock(ctx, "order-9", product.ID, 1)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	got, err = svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reserved != 4 {
		t.Fatalf("reserved changed to %d", got.Reserved)
	}
}

func TestCommitAndRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("commit", func(t *testing.T) {
		svc := newSvc()
		product := mustProduct(t, svc, "C-1", 10)
		reservation, err := svc.ReserveStock(ctx, "c-key", product.ID, 3)
		if err != nil {
			t.Fatal(err)
		}
		committed, err := svc.CommitReservation(ctx, reservation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if committed.Status != domain.ReservationStatusCommitted {
			t.Fatalf("status %s", committed.Status)
		}
		again, err := svc.CommitReservation(ctx, reservation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.Status != domain.ReservationStatusCommitted {
			t.Fatalf("second status %s", again.Status)
		}
		got, err := svc.GetProduct(ctx, product.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.OnHand != 7 || got.Reserved != 0 || got.Available() != 7 {
			t.Fatalf("stock %+v", got)
		}
		if _, err := svc.ReleaseReservation(ctx, reservation.ID); !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("release after commit = %v", err)
		}
	})

	t.Run("release", func(t *testing.T) {
		svc := newSvc()
		product := mustProduct(t, svc, "R-1", 10)
		reservation, err := svc.ReserveStock(ctx, "r-key", product.ID, 3)
		if err != nil {
			t.Fatal(err)
		}
		released, err := svc.ReleaseReservation(ctx, reservation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if released.Status != domain.ReservationStatusReleased {
			t.Fatalf("status %s", released.Status)
		}
		if _, err := svc.ReleaseReservation(ctx, reservation.ID); err != nil {
			t.Fatalf("second release: %v", err)
		}
		got, err := svc.GetProduct(ctx, product.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.OnHand != 10 || got.Reserved != 0 {
			t.Fatalf("stock %+v", got)
		}
		if _, err := svc.CommitReservation(ctx, reservation.ID); !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("commit after release = %v", err)
		}
	})
}

func TestConcurrentReserveNoOversell(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()
	product := mustProduct(t, svc, "RACE-1", 10)

	const attempts = 25
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	failCount := 0
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := svc.ReserveStock(ctx, "k-"+strconv.Itoa(i), product.ID, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okCount++
			case errors.Is(err, domain.ErrInsufficientStock):
				failCount++
			default:
				t.Errorf("reserve %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if okCount != 10 || failCount != attempts-10 {
		t.Fatalf("ok=%d fail=%d", okCount, failCount)
	}
}

func TestConcurrentIdempotencyReservesOnce(t *testing.T) {
	t.Parallel()
	svc := newSvc()
	ctx := context.Background()
	product := mustProduct(t, svc, "RACE-2", 10)

	const attempts = 12
	start := make(chan struct{})
	ids := make([]string, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			reservation, err := svc.ReserveStock(ctx, "same-key", product.ID, 2)
			errs[i] = err
			if err == nil {
				ids[i] = reservation.ID
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Fatalf("id %d = %s, want %s", i, ids[i], ids[0])
		}
	}
	got, err := svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reserved != 2 || got.Available() != 8 {
		t.Fatalf("stock %+v", got)
	}
}
