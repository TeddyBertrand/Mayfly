package receiver

import (
	"log/slog"
	"net/http"
	"time"
)

// NewHandler returns the receiver routes: GET /healthz and POST /webhook.
func NewHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /webhook", handleWebhook(logger))
	return mux
}

// handleHealthz reports that the process is up; it must not depend on external services.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handleWebhook accepts webhook deliveries; verification and parsing come in later issues.
func handleWebhook(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Info("webhook received",
			"delivery_id", r.Header.Get("X-GitHub-Delivery"),
			"event", r.Header.Get("X-GitHub-Event"))
		w.WriteHeader(http.StatusAccepted)
	}
}

// newServer wraps handler in an http.Server with explicit timeouts against slow clients.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
