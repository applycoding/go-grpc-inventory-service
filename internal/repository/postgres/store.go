package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/repository"
)

// Store is a pgx-backed repository.
type Store struct {
	pool *pgxpool.Pool
}

var _ repository.Store = (*Store)(nil)

var errIdempotencyRace = errors.New("idempotency insert race")

// Open connects, pings, and migrates the database.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "inventoryd"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping checks the pool.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) reset(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE reservations, products`)
	return err
}

func scanProduct(row pgx.Row) (domain.Product, error) {
	var product domain.Product
	err := row.Scan(
		&product.ID,
		&product.SKU,
		&product.Name,
		&product.Description,
		&product.PriceCents,
		&product.OnHand,
		&product.Reserved,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	return product, err
}

func scanReservation(row pgx.Row) (domain.Reservation, error) {
	var reservation domain.Reservation
	var status string
	err := row.Scan(
		&reservation.ID,
		&reservation.IdempotencyKey,
		&reservation.ProductID,
		&reservation.Quantity,
		&status,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
	)
	reservation.Status = domain.ReservationStatus(status)
	return reservation, err
}

func mapProductWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		if pgErr.ConstraintName == "products_pkey" {
			return fmt.Errorf("product id already exists: %w", domain.ErrConflict)
		}
		return fmt.Errorf("sku already exists: %w", domain.ErrConflict)
	case "23514":
		return fmt.Errorf("product violates constraints: %w", domain.ErrInvalidArgument)
	default:
		return err
	}
}
