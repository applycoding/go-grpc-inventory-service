package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
)

const productColumns = `id, sku, name, description, price_cents, on_hand, reserved, created_at, updated_at`

// CreateProduct inserts a product row.
func (s *Store) CreateProduct(ctx context.Context, product domain.Product) (domain.Product, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO products (`+productColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		product.ID,
		product.SKU,
		product.Name,
		product.Description,
		product.PriceCents,
		product.OnHand,
		product.Reserved,
		product.CreatedAt,
		product.UpdatedAt,
	)
	if err != nil {
		return domain.Product{}, mapProductWriteErr(err)
	}
	return product, nil
}

// GetProduct loads a product by ID.
func (s *Store) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	product, err := scanProduct(s.pool.QueryRow(ctx, `
		SELECT `+productColumns+` FROM products WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Product{}, err
	}
	return product, nil
}

// ListProducts returns a page ordered by creation time and ID.
func (s *Store) ListProducts(ctx context.Context, offset, limit int) ([]domain.Product, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+productColumns+`
		FROM products
		ORDER BY created_at ASC, id ASC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Product{}
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, product)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AdjustStock updates on-hand stock inside a row lock.
func (s *Store) AdjustStock(ctx context.Context, productID string, delta int32, now time.Time) (domain.Product, error) {
	var updated domain.Product
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		product, err := getProductForUpdate(ctx, tx, productID)
		if err != nil {
			return err
		}
		next, err := domain.ApplyDelta(product.OnHand, product.Reserved, delta)
		if err != nil {
			return err
		}
		product.OnHand = next
		product.UpdatedAt = now.UTC()
		if err := updateProductCounters(ctx, tx, product); err != nil {
			return err
		}
		updated = product
		return nil
	})
	if err != nil {
		return domain.Product{}, err
	}
	return updated, nil
}

func getProductForUpdate(ctx context.Context, tx pgx.Tx, id string) (domain.Product, error) {
	product, err := scanProduct(tx.QueryRow(ctx, `
		SELECT `+productColumns+` FROM products WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, fmt.Errorf("product %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Product{}, err
	}
	return product, nil
}

func updateProductCounters(ctx context.Context, tx pgx.Tx, product domain.Product) error {
	tag, err := tx.Exec(ctx, `
		UPDATE products
		SET on_hand = $2, reserved = $3, updated_at = $4
		WHERE id = $1`,
		product.ID, product.OnHand, product.Reserved, product.UpdatedAt)
	if err != nil {
		return mapProductWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("product %w", domain.ErrNotFound)
	}
	return nil
}
