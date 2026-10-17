package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/arpanbom/go-grpc-inventory-service/internal/app"
	"github.com/arpanbom/go-grpc-inventory-service/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("stopped", "err", err)
		os.Exit(1)
	}
}
