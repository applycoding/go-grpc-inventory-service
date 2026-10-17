package domain

import "time"

// ReservationStatus is the lifecycle of a stock hold.
type ReservationStatus string

const (
	// ReservationStatusHeld means units are reserved and still in the warehouse.
	ReservationStatusHeld ReservationStatus = "held"
	// ReservationStatusCommitted means the units have left the warehouse.
	ReservationStatusCommitted ReservationStatus = "committed"
	// ReservationStatusReleased means the hold was cancelled and units are available again.
	ReservationStatusReleased ReservationStatus = "released"
)

// Reservation is an idempotent hold against a product.
type Reservation struct {
	ID             string
	IdempotencyKey string
	ProductID      string
	Quantity       int32
	Status         ReservationStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ReserveRequest is the persistence input for a new or replayed hold.
type ReserveRequest struct {
	ID             string
	IdempotencyKey string
	ProductID      string
	Quantity       int32
	Now            time.Time
}
