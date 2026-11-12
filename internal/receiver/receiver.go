// Package receiver receives GitHub webhooks and decides when a worker is needed.
package receiver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// dedupeTTL matches GitHub's three-day window for redelivering a webhook.
const dedupeTTL = 72 * time.Hour

// Run serves the receiver on cfg.ListenAddr, handing queued jobs to onQueued, until ctx is cancelled.
func Run(ctx context.Context, cfg config.ReceiverConfig, logger *slog.Logger, onQueued func(Job) error) error {
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}
	return serve(ctx, ln, cfg, logger, onQueued)
}

// serve runs the HTTP server on ln and drains in-flight requests within cfg.ShutdownTimeout once ctx ends.
func serve(ctx context.Context, ln net.Listener, cfg config.ReceiverConfig, logger *slog.Logger, onQueued func(Job) error) error {
	srv := newServer(NewHandler(logger, Webhook{
		Secret:   cfg.WebhookSecret,
		Labels:   cfg.RunnerLabels,
		Dedupe:   NewMemoryDeduper(dedupeTTL),
		OnQueued: onQueued,
	}))

	errCh := make(chan error, 1)
	go func() {
		logger.Info("receiver listening", "addr", ln.Addr().String())
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	// A fresh context: the parent is already cancelled and would abort the drain immediately.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("receiver stopped")
	return nil
}
