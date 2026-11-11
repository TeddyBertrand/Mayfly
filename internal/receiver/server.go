package receiver

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxWebhookBody caps webhook bodies; workflow_job payloads are tens of KB.
const maxWebhookBody = 1 << 20

// NewHandler returns the receiver routes: GET /healthz and POST /webhook signed with secret.
func NewHandler(logger *slog.Logger, secret []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /webhook", handleWebhook(logger, secret))
	return mux
}

// handleHealthz reports that the process is up; it must not depend on external services.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handleWebhook reads the body under a size limit and rejects it unless its signature is valid.
func handleWebhook(logger *slog.Logger, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.With(
			"delivery_id", r.Header.Get("X-GitHub-Delivery"),
			"event", r.Header.Get("X-GitHub-Event"))

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				log.Warn("webhook body too large", "limit", maxWebhookBody)
				http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			log.Warn("webhook body read failed", "err", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if !VerifySignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			log.Warn("webhook signature rejected")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		log.Info("webhook accepted", "bytes", len(body))
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
