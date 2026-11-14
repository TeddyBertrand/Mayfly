// Package httpserve runs Mayfly's HTTP servers with timeouts against slow clients and a graceful shutdown.
package httpserve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// New wraps handler in an http.Server with explicit timeouts against slow clients.
func New(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// Serve runs srv on ln until ctx ends, then drains in-flight requests within shutdownTimeout.
// It returns early with an error if the server stops on its own.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", ln.Addr().String())
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("server shutting down", "timeout", shutdownTimeout.String())
	// A fresh context: the parent is already cancelled and would abort the drain immediately.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
