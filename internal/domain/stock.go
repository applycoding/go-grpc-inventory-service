package domain

import (
	"fmt"
	"math"
)

// AddInt32 adds two int32 values and reports overflow as an invalid argument.
func AddInt32(a, b int32) (int32, error) {
	sum := int64(a) + int64(b)
	if sum > math.MaxInt32 || sum < math.MinInt32 {
		return 0, fmt.Errorf("stock value overflows: %w", ErrInvalidArgument)
	}
	return int32(sum), nil
}

// ApplyDelta returns the next on-hand quantity after delta.
// The result must stay non-negative and must not fall below reserved.
func ApplyDelta(onHand, reserved, delta int32) (int32, error) {
	if delta == 0 {
		return 0, fmt.Errorf("delta must be non-zero: %w", ErrInvalidArgument)
	}
	next, err := AddInt32(onHand, delta)
	if err != nil {
		return 0, err
	}
	if next < 0 || next < reserved {
		return 0, fmt.Errorf("on-hand would fall below reserved quantity: %w", ErrInsufficientStock)
	}
	return next, nil
}

// CanReserve reports whether qty units can be held against the current counters.
func CanReserve(onHand, reserved, qty int32) error {
	if qty <= 0 {
		return fmt.Errorf("quantity must be > 0: %w", ErrInvalidArgument)
	}
	available := int64(onHand) - int64(reserved)
	if int64(qty) > available {
		return fmt.Errorf("only %d available: %w", available, ErrInsufficientStock)
	}
	return nil
}
