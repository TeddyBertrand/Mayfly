package provisioner

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// observe records the job metrics GitHub's timestamps give, for tracked and untracked jobs alike:
// a completed event often arrives after the worker exited and its entry was removed.
func (p *Provisioner) observe(action workflow.Action, job workflow.Job) {
	switch action {
	case workflow.ActionInProgress:
		observeSpan(p.metrics.QueueSeconds, job.CreatedAt, job.StartedAt)
	case workflow.ActionCompleted:
		p.metrics.Jobs.WithLabelValues(job.Conclusion).Inc()
		// A job cancelled before any runner took it never ran, so it has no duration.
		if job.RunnerName != "" {
			observeSpan(p.metrics.JobSeconds, job.StartedAt, job.CompletedAt)
		}
	}
}

// observeSpan records end minus start in h, skipping a span with a missing or reversed timestamp.
func observeSpan(h prometheus.Histogram, start, end time.Time) {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return
	}
	h.Observe(end.Sub(start).Seconds())
}
