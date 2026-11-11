// Command receiver is the entrypoint of the Mayfly receiver component.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
)

// main loads config, runs the receiver until SIGTERM or SIGINT, and exits non-zero on failure.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.LoadReceiver(os.Getenv)
	if err != nil {
		logger.Error("invalid config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	if err := receiver.Run(ctx, cfg, logger); err != nil {
		logger.Error("receiver failed", "err", err)
		os.Exit(1)
	}
}
