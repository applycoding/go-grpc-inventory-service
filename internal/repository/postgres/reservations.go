package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
)

const reservationColumns = `id, idempotency_key, product_id, quantity, status, created_at, updated_at`

// ReserveStock holds units for a product. The idempotency key makes retries safe.
func (s *Store) ReserveStock(ctx context.Context, in domain.ReserveRequest) (domain.Reservation, error) {
	var created domain.Reservation
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := getReservationByKeyForUpdate(ctx, tx, in.IdempotencyKey)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil {
			if existing.ProductID != in.ProductID || existing.Quantity != in.Quantity {
				return fmt.Errorf("idempotency key already used with a different request: %w", domain.ErrIdempotencyConflict)
			}
			created = existing
			return nil
		}

		product, err := getProductForUpdate(ctx, tx, in.ProductID)
		if err != nil {
			return err
		}
		if err := domain.CanReserve(product.OnHand, product.Reserved, in.Quantity); err != nil {
			return err
		}

		now := in.Now.UTC()
		reservation := domain.Reservation{
			ID:             in.ID,
			IdempotencyKey: in.IdempotencyKey,
			ProductID:      in.ProductID,
			Quantity:       in.Quantity,
			Status:         domain.ReservationStatusHeld,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := insertReservation(ctx, tx, reservation); err != nil {
			return err
		}
		product.Reserved += in.Quantity
		product.UpdatedAt = now
		if err := updateProductCounters(ctx, tx, product); err != nil {
			return err
		}
		created = reservation
		return nil
	})
	if errors.Is(err, errIdempotencyRace) {
		return s.reservationAfterRace(ctx, in)
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	return created, nil
}

// GetReservation loads a reservation by ID.
func (s *Store) GetReservation(ctx context.Context, id string) (domain.Reservation, error) {
	reservation, err := scanReservation(s.pool.QueryRow(ctx, `
		SELECT `+reservationColumns+` FROM reservations WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, fmt.Errorf("reservation %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	return reservation, nil
}

// CommitReservation removes held units from the warehouse.
func (s *Store) CommitReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error) {
	return s.transition(ctx, id, now, domain.ReservationStatusCommitted, true)
}

// ReleaseReservation returns held units to available stock.
func (s *Store) ReleaseReservation(ctx context.Context, id string, now time.Time) (domain.Reservation, error) {
	return s.transition(ctx, id, now, domain.ReservationStatusReleased, false)
}

func (s *Store) transition(ctx context.Context, id string, now time.Time, target domain.ReservationStatus, decrementOnHand bool) (domain.Reservation, error) {
	var updated domain.Reservation
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		reservation, err := getReservationForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if reservation.Status == target {
			updated = reservation
			return nil
		}
		if reservation.Status != domain.ReservationStatusHeld {
			return fmt.Errorf("reservation is %s: %w", reservation.Status, domain.ErrInvalidState)
		}
		product, err := getProductForUpdate(ctx, tx, reservation.ProductID)
		if err != nil {
			return err
		}
		if product.Reserved < reservation.Quantity || (decrementOnHand && product.OnHand < reservation.Quantity) {
			return fmt.Errorf("stock counters are inconsistent: %w", domain.ErrInvalidState)
		}
		now = now.UTC()
		product.Reserved -= reservation.Quantity
		if decrementOnHand {
			product.OnHand -= reservation.Quantity
		}
		product.UpdatedAt = now
		if err := updateProductCounters(ctx, tx, product); err != nil {
			return err
		}
		reservation.Status = target
		reservation.UpdatedAt = now
		if err := updateReservationStatus(ctx, tx, reservation); err != nil {
			return err
		}
		updated = reservation
		return nil
	})
	if err != nil {
		return domain.Reservation{}, err
	}
	return updated, nil
}

func (s *Store) reservationAfterRace(ctx context.Context, in domain.ReserveRequest) (domain.Reservation, error) {
	reservation, err := scanReservation(s.pool.QueryRow(ctx, `
		SELECT `+reservationColumns+` FROM reservations WHERE idempotency_key = $1`, in.IdempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, fmt.Errorf("idempotency key disappeared after conflict: %w", domain.ErrConflict)
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	if reservation.ProductID != in.ProductID || reservation.Quantity != in.Quantity {
		return domain.Reservation{}, fmt.Errorf("idempotency key already used with a different request: %w", domain.ErrIdempotencyConflict)
	}
	return reservation, nil
}

func getReservationForUpdate(ctx context.Context, tx pgx.Tx, id string) (domain.Reservation, error) {
	reservation, err := scanReservation(tx.QueryRow(ctx, `
		SELECT `+reservationColumns+` FROM reservations WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, fmt.Errorf("reservation %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	return reservation, nil
}

func getReservationByKeyForUpdate(ctx context.Context, tx pgx.Tx, key string) (domain.Reservation, error) {
	reservation, err := scanReservation(tx.QueryRow(ctx, `
		SELECT `+reservationColumns+` FROM reservations WHERE idempotency_key = $1 FOR UPDATE`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	return reservation, nil
}

func insertReservation(ctx context.Context, tx pgx.Tx, reservation domain.Reservation) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO reservations (`+reservationColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		reservation.ID,
		reservation.IdempotencyKey,
		reservation.ProductID,
		reservation.Quantity,
		string(reservation.Status),
		reservation.CreatedAt,
		reservation.UpdatedAt,
	)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "reservations_idempotency_key_key" {
		return errIdempotencyRace
	}
	return err
}

func updateReservationStatus(ctx context.Context, tx pgx.Tx, reservation domain.Reservation) error {
	tag, err := tx.Exec(ctx, `
		UPDATE reservations
		SET status = $2, updated_at = $3
		WHERE id = $1`,
		reservation.ID, string(reservation.Status), reservation.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("reservation %w", domain.ErrNotFound)
	}
	return nil
}
