package receiver

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxWebhookBody caps webhook bodies; workflow_job payloads are tens of KB.
const maxWebhookBody = 1 << 20

// Webhook holds what the /webhook route needs to accept and hand off jobs.
type Webhook struct {
	Secret []byte
	Labels []string
	Dedupe Deduper
	// OnQueued hands a job off without blocking; an error means it was not accepted.
	OnQueued func(Job) error
	// OnUpdate passes an in_progress or completed job on without blocking.
	OnUpdate func(action string, job Job)
	// Instance is reported on /healthz.
	Instance string
}

// Health is the /healthz response body.
type Health struct {
	Status   string `json:"status"`
	Instance string `json:"instance"`
}

// NewHandler returns the receiver routes: GET /healthz and POST /webhook.
func NewHandler(logger *slog.Logger, wh Webhook) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz(wh.Instance))
	mux.HandleFunc("POST /webhook", handleWebhook(logger, wh))
	return mux
}

// handleHealthz reports that the process is up and which instance it is; it must not depend on external services.
func handleHealthz(instance string) http.HandlerFunc {
	body, _ := json.Marshal(Health{Status: "ok", Instance: instance})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

// handleWebhook reads the body under a size limit, rejects it unless signed, then dispatches it.
func handleWebhook(logger *slog.Logger, wh Webhook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		delivery := r.Header.Get("X-GitHub-Delivery")
		event := r.Header.Get("X-GitHub-Event")
		log := logger.With("delivery_id", delivery, "event", event)

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

		if !VerifySignature(wh.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
			log.Warn("webhook signature rejected")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		status := dispatch(log, wh, event, delivery, body)
		w.WriteHeader(status)
	}
}

// dispatch acts on a verified delivery and returns the HTTP status to answer with.
// Ignored events still get 2xx so GitHub does not mark the delivery as failed.
func dispatch(log *slog.Logger, wh Webhook, event, delivery string, body []byte) int {
	switch event {
	case "ping":
		log.Info("ping received")
		return http.StatusOK
	case "workflow_job":
	default:
		log.Info("event ignored")
		return http.StatusAccepted
	}

	if delivery == "" {
		log.Warn("workflow_job without delivery id")
		return http.StatusBadRequest
	}

	action, job, err := parseWorkflowJob(body)
	if err != nil {
		log.Warn("workflow_job rejected", "err", err)
		return http.StatusBadRequest
	}
	log = log.With("action", action, "job_id", job.ID, "repo", job.Repo)

	switch action {
	case "queued", "in_progress", "completed":
	default:
		log.Info("workflow_job action ignored")
		return http.StatusAccepted
	}
	if !job.MatchesLabels(wh.Labels) {
		log.Info("workflow_job not for mayfly", "labels", job.Labels)
		return http.StatusAccepted
	}
	if wh.Dedupe.Seen(delivery) {
		log.Info("duplicate delivery ignored")
		return http.StatusAccepted
	}

	if action != "queued" {
		wh.OnUpdate(action, job)
		log.Info("workflow_job update handed off", "runner_name", job.RunnerName, "conclusion", job.Conclusion)
		return http.StatusAccepted
	}
	if err := wh.OnQueued(job); err != nil {
		// Un-mark the delivery so GitHub's redelivery is not dropped as a duplicate.
		wh.Dedupe.Forget(delivery)
		log.Error("workflow_job handoff failed", "err", err)
		return http.StatusServiceUnavailable
	}
	log.Info("workflow_job queued", "labels", job.Labels)
	return http.StatusAccepted
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
