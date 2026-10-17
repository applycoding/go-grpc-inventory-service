package domain

import "time"

// Product is a stock-keeping unit.
//
// OnHand is the physical quantity in the warehouse. Reserved is the subset
// currently held by reservations in the held state. Available is OnHand minus
// Reserved. Committing a reservation removes units from both counters. Releasing
// a reservation removes them from Reserved only.
type Product struct {
	ID          string
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	OnHand      int32
	Reserved    int32
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Available is the quantity that can still be reserved.
func (p Product) Available() int32 {
	return p.OnHand - p.Reserved
}
