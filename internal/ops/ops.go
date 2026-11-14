// Package ops serves Mayfly's operational endpoints on a listener that is never exposed to the internet (decision 018).
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/httpserve"
)

// Health is the /healthz response body.
type Health struct {
	Status   string `json:"status"`
	Instance string `json:"instance"`
}

// NewHandler returns the ops routes: GET /healthz, reporting instance so the reaper can tell who is alive.
func NewHandler(instance string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz(instance))
	return mux
}

// Run serves the ops routes on addr until ctx is cancelled, draining requests within shutdownTimeout.
func Run(ctx context.Context, addr string, shutdownTimeout time.Duration, instance string, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	return httpserve.Serve(ctx, httpserve.New(NewHandler(instance)), ln, shutdownTimeout, logger)
}

// handleHealthz reports that the process is up and which instance it is; it must not depend on external services.
func handleHealthz(instance string) http.HandlerFunc {
	body, _ := json.Marshal(Health{Status: "ok", Instance: instance})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}
