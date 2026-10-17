//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/service"
)

func newStore(t *testing.T) *service.Service {
	t.Helper()
	url := os.Getenv("INVENTORY_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("INVENTORY_TEST_DATABASE_URL is required")
	}

	var store *Store
	var err error
	deadline := time.Now().Add(20 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		store, err = Open(ctx, url)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("open postgres: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Cleanup(store.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return service.New(store)
}

func TestPostgresMigrateTwice(t *testing.T) {
	svc := newStore(t)
	ctx := context.Background()
	again, err := Open(ctx, os.Getenv("INVENTORY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	again.Close()

	product, err := svc.CreateProduct(ctx, service.CreateProductInput{
		SKU:        "MIG-1",
		Name:       "Migrated",
		PriceCents: 100,
		OnHand:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if product.SKU != "MIG-1" {
		t.Fatalf("sku %s", product.SKU)
	}
}

func TestPostgresReservationLifecycle(t *testing.T) {
	svc := newStore(t)
	ctx := context.Background()

	product, err := svc.CreateProduct(ctx, service.CreateProductInput{
		SKU:         "PG-MUG",
		Name:        "Postgres mug",
		Description: "local integration row",
		PriceCents:  2200,
		OnHand:      8,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateProduct(ctx, service.CreateProductInput{SKU: "PG-MUG", Name: "Again", PriceCents: 1, OnHand: 1})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate sku error = %v", err)
	}

	first, err := svc.ReserveStock(ctx, "pg-order", product.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.ReserveStock(ctx, "pg-order", product.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id %s", replay.ID)
	}
	if _, err := svc.ReserveStock(ctx, "pg-order", product.ID, 1); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if _, err := svc.ReserveStock(ctx, "pg-too-many", product.ID, 20); !errors.Is(err, domain.ErrInsufficientStock) {
		t.Fatalf("stock error = %v", err)
	}

	committed, err := svc.CommitReservation(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != domain.ReservationStatusCommitted {
		t.Fatalf("status %s", committed.Status)
	}
	if _, err := svc.CommitReservation(ctx, first.ID); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	got, err := svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OnHand != 5 || got.Reserved != 0 {
		t.Fatalf("after commit %+v", got)
	}

	releasedHold, err := svc.ReserveStock(ctx, "pg-release", product.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	released, err := svc.ReleaseReservation(ctx, releasedHold.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != domain.ReservationStatusReleased {
		t.Fatalf("status %s", released.Status)
	}
	got, err = svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OnHand != 5 || got.Reserved != 0 || got.Available() != 5 {
		t.Fatalf("after release %+v", got)
	}
	if _, err := svc.CommitReservation(ctx, releasedHold.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("commit after release = %v", err)
	}

	updated, err := svc.AdjustStock(ctx, product.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if updated.OnHand != 9 {
		t.Fatalf("on hand %d", updated.OnHand)
	}
	if _, err := svc.AdjustStock(ctx, product.ID, -10); !errors.Is(err, domain.ErrInsufficientStock) {
		t.Fatalf("adjust error = %v", err)
	}
}

func TestPostgresListPages(t *testing.T) {
	svc := newStore(t)
	ctx := context.Background()
	want := map[string]struct{}{}
	for i := 0; i < 3; i++ {
		product, err := svc.CreateProduct(ctx, service.CreateProductInput{
			SKU:        fmt.Sprintf("PAGE-%d", i),
			Name:       "Page",
			PriceCents: 1,
			OnHand:     1,
		})
		if err != nil {
			t.Fatal(err)
		}
		want[product.ID] = struct{}{}
	}

	seen := map[string]struct{}{}
	token := ""
	for {
		page, next, err := svc.ListProducts(ctx, 2, token)
		if err != nil {
			t.Fatal(err)
		}
		for _, product := range page {
			if _, ok := seen[product.ID]; ok {
				t.Fatalf("duplicate id %s", product.ID)
			}
			seen[product.ID] = struct{}{}
		}
		if next == "" {
			break
		}
		if next == token {
			t.Fatal("page token did not advance")
		}
		token = next
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %d want %d", len(seen), len(want))
	}
}

func TestPostgresConcurrentReserve(t *testing.T) {
	svc := newStore(t)
	ctx := context.Background()
	product, err := svc.CreateProduct(ctx, service.CreateProductInput{
		SKU:        "RACE",
		Name:       "Race",
		PriceCents: 1,
		OnHand:     10,
	})
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 30
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	failCount := 0
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			_, reserveErr := svc.ReserveStock(ctx, fmt.Sprintf("race-%d", i), product.ID, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case reserveErr == nil:
				okCount++
			case errors.Is(reserveErr, domain.ErrInsufficientStock):
				failCount++
			default:
				t.Errorf("reserve %d: %v", i, reserveErr)
			}
		}(i)
	}
	wg.Wait()
	if okCount != 10 || failCount != attempts-10 {
		t.Fatalf("ok=%d fail=%d", okCount, failCount)
	}
	got, err := svc.GetProduct(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reserved != 10 || got.Available() != 0 || got.OnHand != 10 {
		t.Fatalf("stock %+v", got)
	}
}
