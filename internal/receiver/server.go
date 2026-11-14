package receiver

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// maxWebhookBody caps webhook bodies; workflow_job payloads are tens of KB.
const maxWebhookBody = 1 << 20

// Headers GitHub sets on every webhook delivery.
const (
	headerDelivery  = "X-GitHub-Delivery"
	headerEvent     = "X-GitHub-Event"
	headerSignature = "X-Hub-Signature-256"
)

// Webhook events the receiver acts on; GitHub sends others, which are acknowledged and ignored.
const (
	eventPing        = "ping"
	eventWorkflowJob = "workflow_job"
)

// Webhook holds what the /webhook route needs to accept and hand off jobs.
type Webhook struct {
	Secret []byte
	Labels []string
	Dedupe Deduper
	Handoff
}

// NewHandler returns the receiver's only route, POST /webhook. Ops endpoints live on their own listener (decision 018).
func NewHandler(logger *slog.Logger, wh Webhook) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", handleWebhook(logger, wh))
	return mux
}

// handleWebhook reads the body under a size limit, rejects it unless signed, then dispatches it.
func handleWebhook(logger *slog.Logger, wh Webhook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		delivery := r.Header.Get(headerDelivery)
		event := r.Header.Get(headerEvent)
		log := logger.With(logkey.DeliveryID, delivery, logkey.Event, event)

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				log.Warn("webhook body too large", logkey.Limit, maxWebhookBody)
				http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			log.Warn("webhook body read failed", logkey.Err, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if !VerifySignature(wh.Secret, body, r.Header.Get(headerSignature)) {
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
	case eventPing:
		log.Info("ping received")
		return http.StatusOK
	case eventWorkflowJob:
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
		log.Warn("workflow_job rejected", logkey.Err, err)
		return http.StatusBadRequest
	}
	log = log.With(logkey.Action, action, logkey.JobID, job.ID, logkey.Repo, job.Repo)

	switch action {
	case workflow.ActionQueued, workflow.ActionInProgress, workflow.ActionCompleted:
	default:
		log.Info("workflow_job action ignored")
		return http.StatusAccepted
	}
	if !workflow.MatchesLabels(job.Labels, wh.Labels) {
		log.Info("workflow_job not for mayfly", logkey.Labels, job.Labels)
		return http.StatusAccepted
	}
	if wh.Dedupe.Seen(delivery) {
		log.Info("duplicate delivery ignored")
		return http.StatusAccepted
	}

	if action != workflow.ActionQueued {
		wh.OnUpdate(action, job)
		log.Info("workflow_job update handed off", logkey.RunnerName, job.RunnerName, logkey.Conclusion, job.Conclusion)
		return http.StatusAccepted
	}
	if err := wh.OnQueued(job); err != nil {
		// Un-mark the delivery so GitHub's redelivery is not dropped as a duplicate.
		wh.Dedupe.Forget(delivery)
		log.Error("workflow_job handoff failed", logkey.Err, err)
		return http.StatusServiceUnavailable
	}
	log.Info("workflow_job queued", logkey.Labels, job.Labels)
	return http.StatusAccepted
}
