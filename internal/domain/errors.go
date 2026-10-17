package domain

import "errors"

var (
	// ErrNotFound is returned when a product or reservation does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a unique field, such as SKU, is already used.
	ErrConflict = errors.New("conflict")
	// ErrInsufficientStock is returned when a change would reserve or remove more units than are available.
	ErrInsufficientStock = errors.New("insufficient stock")
	// ErrIdempotencyConflict is returned when an idempotency key is reused with a different reservation payload.
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	// ErrInvalidState is returned when a reservation is not in the state required for the operation.
	ErrInvalidState = errors.New("invalid reservation state")
	// ErrInvalidArgument is returned when a request field fails validation.
	ErrInvalidArgument = errors.New("invalid argument")
)
