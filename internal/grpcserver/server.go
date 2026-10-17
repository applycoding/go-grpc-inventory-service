package grpcserver

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/arpanbom/go-grpc-inventory-service/gen/inventory/v1"
	"github.com/arpanbom/go-grpc-inventory-service/internal/domain"
	"github.com/arpanbom/go-grpc-inventory-service/internal/service"
)

// Server adapts the inventory service to gRPC.
type Server struct {
	inventoryv1.UnimplementedInventoryServiceServer
	svc    *service.Service
	logger *slog.Logger
}

// New returns a gRPC inventory server.
func New(svc *service.Service, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{svc: svc, logger: logger}
}

// Register attaches the inventory service, health, and server reflection.
func Register(server *grpc.Server, svc *service.Service, logger *slog.Logger) *health.Server {
	inventoryv1.RegisterInventoryServiceServer(server, New(svc, logger))
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(inventoryv1.InventoryService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
	return healthServer
}

// CreateProduct stores a product.
func (s *Server) CreateProduct(ctx context.Context, req *inventoryv1.CreateProductRequest) (*inventoryv1.CreateProductResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	product, err := s.svc.CreateProduct(ctx, service.CreateProductInput{
		SKU:         req.GetSku(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		PriceCents:  req.GetPriceCents(),
		OnHand:      req.GetOnHand(),
	})
	if err != nil {
		return nil, s.fail("CreateProduct", err)
	}
	return &inventoryv1.CreateProductResponse{Product: toProtoProduct(product)}, nil
}

// GetProduct loads a product.
func (s *Server) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest) (*inventoryv1.GetProductResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	product, err := s.svc.GetProduct(ctx, req.GetId())
	if err != nil {
		return nil, s.fail("GetProduct", err)
	}
	return &inventoryv1.GetProductResponse{Product: toProtoProduct(product)}, nil
}

// ListProducts returns a page of products.
func (s *Server) ListProducts(ctx context.Context, req *inventoryv1.ListProductsRequest) (*inventoryv1.ListProductsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	products, next, err := s.svc.ListProducts(ctx, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, s.fail("ListProducts", err)
	}
	out := make([]*inventoryv1.Product, 0, len(products))
	for _, product := range products {
		out = append(out, toProtoProduct(product))
	}
	return &inventoryv1.ListProductsResponse{Products: out, NextPageToken: next}, nil
}

// AdjustStock changes on-hand quantity.
func (s *Server) AdjustStock(ctx context.Context, req *inventoryv1.AdjustStockRequest) (*inventoryv1.AdjustStockResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	product, err := s.svc.AdjustStock(ctx, req.GetProductId(), req.GetDelta())
	if err != nil {
		return nil, s.fail("AdjustStock", err)
	}
	return &inventoryv1.AdjustStockResponse{Product: toProtoProduct(product)}, nil
}

// ReserveStock holds units using an idempotency key.
func (s *Server) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reservation, err := s.svc.ReserveStock(ctx, req.GetIdempotencyKey(), req.GetProductId(), req.GetQuantity())
	if err != nil {
		return nil, s.fail("ReserveStock", err)
	}
	protoReservation, err := toProtoReservation(reservation)
	if err != nil {
		return nil, s.fail("ReserveStock", err)
	}
	return &inventoryv1.ReserveStockResponse{Reservation: protoReservation}, nil
}

// GetReservation loads a reservation.
func (s *Server) GetReservation(ctx context.Context, req *inventoryv1.GetReservationRequest) (*inventoryv1.GetReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reservation, err := s.svc.GetReservation(ctx, req.GetId())
	if err != nil {
		return nil, s.fail("GetReservation", err)
	}
	protoReservation, err := toProtoReservation(reservation)
	if err != nil {
		return nil, s.fail("GetReservation", err)
	}
	return &inventoryv1.GetReservationResponse{Reservation: protoReservation}, nil
}

// CommitReservation finalizes a hold.
func (s *Server) CommitReservation(ctx context.Context, req *inventoryv1.CommitReservationRequest) (*inventoryv1.CommitReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reservation, err := s.svc.CommitReservation(ctx, req.GetId())
	if err != nil {
		return nil, s.fail("CommitReservation", err)
	}
	protoReservation, err := toProtoReservation(reservation)
	if err != nil {
		return nil, s.fail("CommitReservation", err)
	}
	return &inventoryv1.CommitReservationResponse{Reservation: protoReservation}, nil
}

// ReleaseReservation cancels a hold.
func (s *Server) ReleaseReservation(ctx context.Context, req *inventoryv1.ReleaseReservationRequest) (*inventoryv1.ReleaseReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reservation, err := s.svc.ReleaseReservation(ctx, req.GetId())
	if err != nil {
		return nil, s.fail("ReleaseReservation", err)
	}
	protoReservation, err := toProtoReservation(reservation)
	if err != nil {
		return nil, s.fail("ReleaseReservation", err)
	}
	return &inventoryv1.ReleaseReservationResponse{Reservation: protoReservation}, nil
}

func (s *Server) fail(method string, err error) error {
	if err == nil {
		return nil
	}
	code := codeFor(err)
	if code == codes.Internal {
		s.logger.Error("rpc failed", "method", method, "err", err)
		return status.Error(codes.Internal, "internal error")
	}
	return status.Error(code, err.Error())
}

func codeFor(err error) codes.Code {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return codes.NotFound
	case errors.Is(err, domain.ErrConflict):
		return codes.AlreadyExists
	case errors.Is(err, domain.ErrInsufficientStock),
		errors.Is(err, domain.ErrIdempotencyConflict),
		errors.Is(err, domain.ErrInvalidState):
		return codes.FailedPrecondition
	case errors.Is(err, domain.ErrInvalidArgument):
		return codes.InvalidArgument
	default:
		return codes.Internal
	}
}
