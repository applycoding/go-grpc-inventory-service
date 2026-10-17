package repository

import (
	"context"
	"time"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
)

// Store persists products and reservations.
// The in-memory implementation is the test fake and the local fallback.
// The Postgres implementation is used when a database URL is configured.
type Store interface {
	Ping(ctx context.Context) error
	CreateProduct(ctx context.Context, product domain.Product) (domain.Product, error)
	GetProduct(ctx context.Context, id string) (domain.Product, error)
	ListProducts(ctx context.Context, offset, limit int) ([]domain.Product, error)
	AdjustStock(ctx context.Context, productID string, delta int32, now time.Time) (domain.Product, error)
	ReserveStock(ctx context.Context, in domain.ReserveRequest) (domain.Reservation, error)
	GetReservation(ctx context.Context, id string) (domain.Reservation, error)
	CommitReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error)
	ReleaseReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error)
}
