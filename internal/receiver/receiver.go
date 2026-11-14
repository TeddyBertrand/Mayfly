// Package receiver receives GitHub webhooks and decides when a worker is needed.
package receiver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/httpserve"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// dedupeTTL matches GitHub's three-day window for redelivering a webhook.
const dedupeTTL = 72 * time.Hour

// Handoff is where the receiver sends accepted jobs: new ones to OnQueued, later actions to OnUpdate.
type Handoff struct {
	// OnQueued hands a job off without blocking; an error means it was not accepted.
	OnQueued func(workflow.Job) error
	// OnUpdate passes an in_progress or completed job on without blocking.
	OnUpdate func(action workflow.Action, job workflow.Job)
}

// Run serves the webhook on cfg.ListenAddr, handing jobs to h, until ctx is cancelled.
func Run(ctx context.Context, cfg config.ReceiverConfig, logger *slog.Logger, h Handoff) error {
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}
	handler := NewHandler(logger, Webhook{
		Secret:  cfg.WebhookSecret,
		Labels:  cfg.RunnerLabels,
		Dedupe:  NewMemoryDeduper(dedupeTTL),
		Handoff: h,
	})
	return httpserve.Serve(ctx, httpserve.New(handler), ln, cfg.ShutdownTimeout, logger)
}
