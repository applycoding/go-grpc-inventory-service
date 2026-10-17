package domain

import (
	"errors"
	"math"
	"testing"
)

func TestApplyDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		onHand   int32
		reserved int32
		delta    int32
		want     int32
		wantErr  error
	}{
		{name: "add", onHand: 5, reserved: 1, delta: 2, want: 7},
		{name: "remove down to reserved", onHand: 5, reserved: 2, delta: -3, want: 2},
		{name: "zero delta", onHand: 5, reserved: 0, delta: 0, wantErr: ErrInvalidArgument},
		{name: "below zero", onHand: 1, reserved: 0, delta: -2, wantErr: ErrInsufficientStock},
		{name: "below reserved", onHand: 5, reserved: 4, delta: -2, wantErr: ErrInsufficientStock},
		{name: "overflow", onHand: math.MaxInt32, reserved: 0, delta: 1, wantErr: ErrInvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ApplyDelta(tt.onHand, tt.reserved, tt.delta)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("on-hand = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCanReserve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		onHand   int32
		reserved int32
		qty      int32
		wantErr  error
	}{
		{name: "exact available", onHand: 5, reserved: 2, qty: 3},
		{name: "too many", onHand: 5, reserved: 2, qty: 4, wantErr: ErrInsufficientStock},
		{name: "non positive", onHand: 5, reserved: 0, qty: 0, wantErr: ErrInvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CanReserve(tt.onHand, tt.reserved, tt.qty)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
