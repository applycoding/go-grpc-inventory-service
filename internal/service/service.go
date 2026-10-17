// Package service implements inventory validation and reservation rules.
package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
	maxSKULen       = 64
	maxNameLen      = 200
	maxDescription  = 2000
	maxKeyLen       = 128
	maxQuantity     = 1_000_000
)

var skuPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// CreateProductInput is the validated shape of a new product.
type CreateProductInput struct {
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	OnHand      int32
}

// Service coordinates the store.
type Service struct {
	store repository.Store
	now   func() time.Time
	newID func() string
}

// New returns a service that uses wall-clock time and random UUIDs.
func New(store repository.Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: uuid.NewString,
	}
}

// CreateProduct validates and stores a product.
func (s *Service) CreateProduct(ctx context.Context, in CreateProductInput) (domain.Product, error) {
	sku := strings.TrimSpace(in.SKU)
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	if err := validateProductFields(sku, name, description, in.PriceCents, in.OnHand); err != nil {
		return domain.Product{}, err
	}
	now := s.now()
	product := domain.Product{
		ID:          s.newID(),
		SKU:         sku,
		Name:        name,
		Description: description,
		PriceCents:  in.PriceCents,
		OnHand:      in.OnHand,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.store.CreateProduct(ctx, product)
}

// GetProduct returns one product.
func (s *Service) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Product{}, fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.store.GetProduct(ctx, id)
}

// ListProducts returns a page of products and an opaque offset token for the next page.
func (s *Service) ListProducts(ctx context.Context, pageSize int32, pageToken string) ([]domain.Product, string, error) {
	offset, limit, err := normalizePage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.store.ListProducts(ctx, offset, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.Itoa(offset + limit)
	}
	if rows == nil {
		rows = []domain.Product{}
	}
	return rows, next, nil
}

// AdjustStock adds or removes on-hand units.
func (s *Service) AdjustStock(ctx context.Context, productID string, delta int32) (domain.Product, error) {
	if strings.TrimSpace(productID) == "" {
		return domain.Product{}, fmt.Errorf("product_id is required: %w", domain.ErrInvalidArgument)
	}
	if delta == 0 {
		return domain.Product{}, fmt.Errorf("delta must be non-zero: %w", domain.ErrInvalidArgument)
	}
	return s.store.AdjustStock(ctx, productID, delta, s.now())
}

// ReserveStock holds stock. Retries with the same key and payload return the original reservation.
func (s *Service) ReserveStock(ctx context.Context, key, productID string, quantity int32) (domain.Reservation, error) {
	key = strings.TrimSpace(key)
	productID = strings.TrimSpace(productID)
	if err := validateKey(key); err != nil {
		return domain.Reservation{}, err
	}
	if productID == "" {
		return domain.Reservation{}, fmt.Errorf("product_id is required: %w", domain.ErrInvalidArgument)
	}
	if quantity <= 0 || quantity > maxQuantity {
		return domain.Reservation{}, fmt.Errorf("quantity must be between 1 and %d: %w", maxQuantity, domain.ErrInvalidArgument)
	}
	return s.store.ReserveStock(ctx, domain.ReserveRequest{
		ID:             s.newID(),
		IdempotencyKey: key,
		ProductID:      productID,
		Quantity:       quantity,
		Now:            s.now(),
	})
}

// GetReservation returns one reservation.
func (s *Service) GetReservation(ctx context.Context, id string) (domain.Reservation, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Reservation{}, fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.store.GetReservation(ctx, id)
}

// CommitReservation finalizes a hold and removes the units from the warehouse.
func (s *Service) CommitReservation(ctx context.Context, id string) (domain.Reservation, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Reservation{}, fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.store.CommitReservation(ctx, id, s.now())
}

// ReleaseReservation cancels a hold and returns the units to available stock.
func (s *Service) ReleaseReservation(ctx context.Context, id string) (domain.Reservation, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Reservation{}, fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.store.ReleaseReservation(ctx, id, s.now())
}

func validateProductFields(sku, name, description string, priceCents int64, onHand int32) error {
	if sku == "" || len(sku) > maxSKULen || !skuPattern.MatchString(sku) {
		return fmt.Errorf("sku must be 1-%d characters of letters, digits, dot, underscore, or hyphen: %w", maxSKULen, domain.ErrInvalidArgument)
	}
	if name == "" || len(name) > maxNameLen {
		return fmt.Errorf("name must be 1-%d characters: %w", maxNameLen, domain.ErrInvalidArgument)
	}
	if len(description) > maxDescription {
		return fmt.Errorf("description must be at most %d characters: %w", maxDescription, domain.ErrInvalidArgument)
	}
	if priceCents < 0 {
		return fmt.Errorf("price_cents must be >= 0: %w", domain.ErrInvalidArgument)
	}
	if onHand < 0 {
		return fmt.Errorf("on_hand must be >= 0: %w", domain.ErrInvalidArgument)
	}
	return nil
}

func validateKey(key string) error {
	if key == "" || len(key) > maxKeyLen || strings.ContainsAny(key, " \t\r\n") {
		return fmt.Errorf("idempotency_key must be 1-%d characters without whitespace: %w", maxKeyLen, domain.ErrInvalidArgument)
	}
	return nil
}

func normalizePage(pageSize int32, pageToken string) (int, int, error) {
	if pageSize < 0 {
		return 0, 0, fmt.Errorf("page_size must be >= 0: %w", domain.ErrInvalidArgument)
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return 0, 0, fmt.Errorf("page_size must be <= %d: %w", maxPageSize, domain.ErrInvalidArgument)
	}
	if pageToken == "" {
		return 0, int(pageSize), nil
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		return 0, 0, fmt.Errorf("page_token is invalid: %w", domain.ErrInvalidArgument)
	}
	return offset, int(pageSize), nil
}
