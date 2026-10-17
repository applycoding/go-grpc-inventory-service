// Package memory is a process-local Store.
// Unit tests use it as a repository fake. The server uses it when no database URL is set.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
)

// Store keeps products and reservations in maps guarded by one mutex.
type Store struct {
	mu           sync.Mutex
	products     map[string]domain.Product
	bySKU        map[string]string
	order        []string
	reservations map[string]domain.Reservation
	byKey        map[string]string
}

var _ repository.Store = (*Store)(nil)

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		products:     map[string]domain.Product{},
		bySKU:        map[string]string{},
		reservations: map[string]domain.Reservation{},
		byKey:        map[string]string{},
	}
}

// Ping reports that the process-local store is ready.
func (s *Store) Ping(context.Context) error {
	return nil
}

// CreateProduct inserts a product. SKU and ID must be unique.
func (s *Store) CreateProduct(ctx context.Context, product domain.Product) (domain.Product, error) {
	if err := ctx.Err(); err != nil {
		return domain.Product{}, err
	}
	if err := validateNewProduct(product); err != nil {
		return domain.Product{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.products[product.ID]; ok {
		return domain.Product{}, fmt.Errorf("product id already exists: %w", domain.ErrConflict)
	}
	if _, ok := s.bySKU[product.SKU]; ok {
		return domain.Product{}, fmt.Errorf("sku already exists: %w", domain.ErrConflict)
	}

	s.products[product.ID] = product
	s.bySKU[product.SKU] = product.ID
	s.order = append(s.order, product.ID)
	return product, nil
}

// GetProduct returns a product by ID.
func (s *Store) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	if err := ctx.Err(); err != nil {
		return domain.Product{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	product, ok := s.products[id]
	if !ok {
		return domain.Product{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	return product, nil
}

// ListProducts returns products in creation order.
func (s *Store) ListProducts(ctx context.Context, offset, limit int) ([]domain.Product, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if offset > len(s.order) {
		offset = len(s.order)
	}
	end := offset + limit
	if end > len(s.order) {
		end = len(s.order)
	}
	out := make([]domain.Product, 0, end-offset)
	for _, id := range s.order[offset:end] {
		out = append(out, s.products[id])
	}
	return out, nil
}

// AdjustStock changes on-hand stock without dropping it below the reserved quantity.
func (s *Store) AdjustStock(ctx context.Context, productID string, delta int32, now time.Time) (domain.Product, error) {
	if err := ctx.Err(); err != nil {
		return domain.Product{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[productID]
	if !ok {
		return domain.Product{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	next, err := domain.ApplyDelta(product.OnHand, product.Reserved, delta)
	if err != nil {
		return domain.Product{}, err
	}
	product.OnHand = next
	product.UpdatedAt = now.UTC()
	s.products[productID] = product
	return product, nil
}

// ReserveStock holds units. A repeated idempotency key returns the original reservation
// when the payload matches, and a conflict when it does not.
func (s *Store) ReserveStock(ctx context.Context, in domain.ReserveRequest) (domain.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Reservation{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existingID, ok := s.byKey[in.IdempotencyKey]; ok {
		existing := s.reservations[existingID]
		if !sameReserve(existing, in) {
			return domain.Reservation{}, fmt.Errorf("idempotency key already used with a different request: %w", domain.ErrIdempotencyConflict)
		}
		return existing, nil
	}

	product, ok := s.products[in.ProductID]
	if !ok {
		return domain.Reservation{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	if err := domain.CanReserve(product.OnHand, product.Reserved, in.Quantity); err != nil {
		return domain.Reservation{}, err
	}

	now := in.Now.UTC()
	product.Reserved += in.Quantity
	product.UpdatedAt = now
	s.products[product.ID] = product

	reservation := domain.Reservation{
		ID:             in.ID,
		IdempotencyKey: in.IdempotencyKey,
		ProductID:      in.ProductID,
		Quantity:       in.Quantity,
		Status:         domain.ReservationStatusHeld,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.reservations[reservation.ID] = reservation
	s.byKey[reservation.IdempotencyKey] = reservation.ID
	return reservation, nil
}

// GetReservation returns a reservation by ID.
func (s *Store) GetReservation(ctx context.Context, id string) (domain.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Reservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reservation, ok := s.reservations[id]
	if !ok {
		return domain.Reservation{}, fmt.Errorf("reservation %w", domain.ErrNotFound)
	}
	return reservation, nil
}

// CommitReservation marks a held reservation committed and removes the units from stock.
// Committing an already committed reservation returns it unchanged.
func (s *Store) CommitReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Reservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transition(id, domain.ReservationStatusCommitted, now, true)
}

// ReleaseReservation cancels a held reservation and returns units to available stock.
// Releasing an already released reservation returns it unchanged.
func (s *Store) ReleaseReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Reservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transition(id, domain.ReservationStatusReleased, now, false)
}

func (s *Store) transition(id string, target domain.ReservationStatus, now time.Time, decrementOnHand bool) (domain.Reservation, error) {
	reservation, ok := s.reservations[id]
	if !ok {
		return domain.Reservation{}, fmt.Errorf("reservation %w", domain.ErrNotFound)
	}
	if reservation.Status == target {
		return reservation, nil
	}
	if reservation.Status != domain.ReservationStatusHeld {
		return domain.Reservation{}, fmt.Errorf("reservation is %s: %w", reservation.Status, domain.ErrInvalidState)
	}

	product, ok := s.products[reservation.ProductID]
	if !ok {
		return domain.Reservation{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	if product.Reserved < reservation.Quantity || (decrementOnHand && product.OnHand < reservation.Quantity) {
		return domain.Reservation{}, fmt.Errorf("stock counters are inconsistent: %w", domain.ErrInvalidState)
	}

	now = now.UTC()
	product.Reserved -= reservation.Quantity
	if decrementOnHand {
		product.OnHand -= reservation.Quantity
	}
	product.UpdatedAt = now
	s.products[product.ID] = product

	reservation.Status = target
	reservation.UpdatedAt = now
	s.reservations[id] = reservation
	return reservation, nil
}

func validateNewProduct(product domain.Product) error {
	if product.ID == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	if product.SKU == "" {
		return fmt.Errorf("sku is required: %w", domain.ErrInvalidArgument)
	}
	if product.Name == "" {
		return fmt.Errorf("name is required: %w", domain.ErrInvalidArgument)
	}
	if product.PriceCents < 0 {
		return fmt.Errorf("price_cents must be >= 0: %w", domain.ErrInvalidArgument)
	}
	if product.OnHand < 0 {
		return fmt.Errorf("on_hand must be >= 0: %w", domain.ErrInvalidArgument)
	}
	if product.Reserved != 0 {
		return fmt.Errorf("reserved must be 0 on create: %w", domain.ErrInvalidArgument)
	}
	return nil
}

func sameReserve(existing domain.Reservation, in domain.ReserveRequest) bool {
	return existing.ProductID == in.ProductID && existing.Quantity == in.Quantity
}
