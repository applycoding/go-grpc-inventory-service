package grpcserver

import (
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	inventoryv1 "github.com/arpanbom/go-grpc-inventory-service/gen/inventory/v1"
	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
)

func toProtoProduct(product domain.Product) *inventoryv1.Product {
	return &inventoryv1.Product{
		Id:          product.ID,
		Sku:         product.SKU,
		Name:        product.Name,
		Description: product.Description,
		PriceCents:  product.PriceCents,
		OnHand:      product.OnHand,
		Reserved:    product.Reserved,
		Available:   product.Available(),
		CreatedAt:   timestamppb.New(product.CreatedAt),
		UpdatedAt:   timestamppb.New(product.UpdatedAt),
	}
}

func toProtoReservation(reservation domain.Reservation) (*inventoryv1.Reservation, error) {
	status, err := toProtoStatus(reservation.Status)
	if err != nil {
		return nil, err
	}
	return &inventoryv1.Reservation{
		Id:             reservation.ID,
		IdempotencyKey: reservation.IdempotencyKey,
		ProductId:      reservation.ProductID,
		Quantity:       reservation.Quantity,
		Status:         status,
		CreatedAt:      timestamppb.New(reservation.CreatedAt),
		UpdatedAt:      timestamppb.New(reservation.UpdatedAt),
	}, nil
}

func toProtoStatus(status domain.ReservationStatus) (inventoryv1.ReservationStatus, error) {
	switch status {
	case domain.ReservationStatusHeld:
		return inventoryv1.ReservationStatus_RESERVATION_STATUS_HELD, nil
	case domain.ReservationStatusCommitted:
		return inventoryv1.ReservationStatus_RESERVATION_STATUS_COMMITTED, nil
	case domain.ReservationStatusReleased:
		return inventoryv1.ReservationStatus_RESERVATION_STATUS_RELEASED, nil
	default:
		return inventoryv1.ReservationStatus_RESERVATION_STATUS_UNSPECIFIED, fmt.Errorf("unknown reservation status %q", status)
	}
}
