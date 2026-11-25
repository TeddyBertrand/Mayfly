// Package reaper destroys leaked or stuck workers as a safety net.
package reaper

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

// GitHub is what the reaper reads and deletes on GitHub.
type GitHub interface {
	Runner(ctx context.Context, installationID int64, repo string, runnerID int64) (github.Runner, error)
	Repos(ctx context.Context) ([]github.Repo, error)
	Runners(ctx context.Context, installationID int64, repo string) ([]github.RunnerRegistration, error)
	DeleteRunner(ctx context.Context, installationID int64, repo string, runnerID int64) error
	QueuedJobs(ctx context.Context, installationID int64, repo string) ([]github.QueuedJob, error)
}

// Owner returns the instance ID of the live mayfly process, or an error if it does not answer.
type Owner func(ctx context.Context) (string, error)

// Options tune when a worker counts as orphaned.
type Options struct {
	// MaxAge removes any worker older than this, whatever else is true.
	MaxAge time.Duration
	// Grace skips the GitHub check for younger workers, whose runner may not be online yet.
	Grace time.Duration
	// MissedProbes is how many cycles in a row the owner must fail to answer before its workers are removed.
	MissedProbes int
	// StaleAfter is how long a mayfly registration must stay offline with no container before it is deleted.
	// It must outlast an image pull, since a registration exists before its container does.
	StaleAfter time.Duration
	// StuckAfter is how long a job may wait in queued before it is logged as stuck.
	StuckAfter time.Duration
	// Labels are the labels a job must request to be mayfly's, as in the receiver.
	Labels []string
	// Metrics receives what each sweep did; nil uses unregistered ones.
	Metrics *metrics.Reaper
}

// Removal is one worker the reaper removed and why.
type Removal struct {
	Worker worker.Info
	Reason string
}

// Stuck is a mayfly job waiting in queued for longer than StuckAfter.
type Stuck struct {
	Repo   string
	Job    github.QueuedJob
	Reason string
}

// Report is what one sweep did.
type Report struct {
	Removed      []Removal
	Deregistered []github.RunnerRegistration
	Stuck        []Stuck
}

// Reaper removes orphaned workers and stale registrations, and reports stuck jobs.
// It only acts on positive evidence: when unsure, it waits for the next cycle.
type Reaper struct {
	workers worker.Inventory
	gh      GitHub
	owner   Owner
	opts    Options
	logger  *slog.Logger
	now     func() time.Time
	missed  int
	// staleSince is when each candidate registration was first seen stale; lost on restart, which only delays deletion.
	staleSince map[int64]time.Time
	// reported holds stuck jobs already logged, so each is logged once.
	reported map[int64]bool
	metrics  *metrics.Reaper
}

// New returns a Reaper over workers, checking GitHub through gh and the live owner through owner.
func New(workers worker.Inventory, gh GitHub, owner Owner, opts Options, logger *slog.Logger) *Reaper {
	m := opts.Metrics
	if m == nil {
		m = metrics.NewReaper(nil)
	}
	presetReasons(m)
	return &Reaper{
		workers: workers, gh: gh, owner: owner, opts: opts, logger: logger, now: time.Now,
		staleSince: map[int64]time.Time{}, reported: map[int64]bool{}, metrics: m,
	}
}

// Run sweeps once at start, then every interval, until ctx ends.
func (r *Reaper) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
			r.logger.Error("reaper sweep failed", logkey.Err, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sweep runs one cycle: orphaned containers first, then stale registrations, then stuck jobs.
// Containers go first so a worker removed now makes its registration a candidate in the same cycle.
// An error means the container list failed and nothing else ran; GitHub failures only skip their pass.
func (r *Reaper) Sweep(ctx context.Context) (Report, error) {
	workers, err := r.workers.List(ctx)
	if err != nil {
		r.metrics.SweepFailures.Inc()
		return Report{}, fmt.Errorf("list workers: %w", err)
	}
	r.metrics.LastSweep.Set(float64(r.now().Unix()))
	live, ownerErr := r.owner(ctx)
	if ownerErr != nil {
		r.missed++
		r.logger.Warn("owner did not answer", logkey.Err, ownerErr, logkey.Missed, r.missed, logkey.Limit, r.opts.MissedProbes)
	} else {
		r.missed = 0
	}

	var rep Report
	claimed := map[int64]bool{}
	for _, w := range workers {
		reason := r.reason(ctx, w, live, ownerErr)
		if reason == "" {
			claimed[w.RunnerID] = true
			continue
		}
		log := r.logger.With(logkey.ContainerID, worker.ShortID(w.ID), logkey.JobID, w.JobID, logkey.RunnerName, w.RunnerName, logkey.Reason, reason)
		if err := r.workers.Remove(ctx, w.ID); err != nil {
			log.Error("worker remove failed", logkey.Err, err)
			claimed[w.RunnerID] = true
			continue
		}
		r.metrics.WorkersRemoved.WithLabelValues(reasonLabels[reason]).Inc()
		log.Info("orphaned worker removed", logkey.Age, r.now().Sub(w.Created).Round(time.Second).String())
		rep.Removed = append(rep.Removed, Removal{Worker: w, Reason: reason})
	}

	repos, err := r.gh.Repos(ctx)
	if err != nil {
		r.logger.Warn("github repos unavailable, registrations and stuck jobs skipped", logkey.Err, err)
		return rep, nil
	}
	rep.Deregistered = r.sweepRegistrations(ctx, repos, claimed)
	rep.Stuck = r.sweepStuck(ctx, repos, ownerErr)
	return rep, nil
}
