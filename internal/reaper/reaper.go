// Package reaper destroys leaked or stuck workers as a safety net.
package reaper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// Reasons a worker is removed, as logged and returned by Sweep.
const (
	ReasonMaxAge           = "max age exceeded"
	ReasonOwnerGone        = "owner instance gone"
	ReasonOwnerUnreachable = "owner unreachable"
	ReasonRunnerGone       = "runner registration gone"
	ReasonRunnerOffline    = "runner offline"
)

// Workers lists and removes worker containers.
type Workers interface {
	List(ctx context.Context) ([]docker.Worker, error)
	Remove(ctx context.Context, id string) error
}

// Runners looks up a runner registration on GitHub.
type Runners interface {
	Runner(ctx context.Context, installationID int64, repo string, runnerID int64) (github.Runner, error)
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
}

// Removal is one worker the reaper removed and why.
type Removal struct {
	Worker docker.Worker
	Reason string
}

// Reaper removes orphaned workers. It only acts on positive evidence: when unsure, it leaves the worker for the next cycle.
type Reaper struct {
	workers Workers
	runners Runners
	owner   Owner
	opts    Options
	logger  *slog.Logger
	now     func() time.Time
	missed  int
}

// New returns a Reaper over workers, checking runners on GitHub and the live owner through owner.
func New(workers Workers, runners Runners, owner Owner, opts Options, logger *slog.Logger) *Reaper {
	return &Reaper{workers: workers, runners: runners, owner: owner, opts: opts, logger: logger, now: time.Now}
}

// Run sweeps once at start, then every interval, until ctx ends.
func (r *Reaper) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
			r.logger.Error("reaper sweep failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sweep runs one cycle: it lists workers, decides which are orphaned, and removes them.
// It returns the workers it removed; an error means the list itself failed.
func (r *Reaper) Sweep(ctx context.Context) ([]Removal, error) {
	workers, err := r.workers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	live, ownerErr := r.owner(ctx)
	if ownerErr != nil {
		r.missed++
		r.logger.Warn("owner did not answer", "err", ownerErr, "missed", r.missed, "limit", r.opts.MissedProbes)
	} else {
		r.missed = 0
	}

	var removed []Removal
	for _, w := range workers {
		reason := r.reason(ctx, w, live, ownerErr)
		if reason == "" {
			continue
		}
		log := r.logger.With("container_id", shortID(w.ID), "job_id", w.JobID, "runner_name", w.RunnerName, "reason", reason)
		if err := r.workers.Remove(ctx, w.ID); err != nil {
			log.Error("worker remove failed", "err", err)
			continue
		}
		log.Info("orphaned worker removed", "age", r.now().Sub(w.Created).Round(time.Second).String())
		removed = append(removed, Removal{Worker: w, Reason: reason})
	}
	return removed, nil
}

// reason returns why w should be removed, or "" to keep it. live and ownerErr are this cycle's owner probe.
func (r *Reaper) reason(ctx context.Context, w docker.Worker, live string, ownerErr error) string {
	age := r.now().Sub(w.Created)
	switch {
	case age > r.opts.MaxAge:
		return ReasonMaxAge
	case ownerErr == nil && w.Owner != live:
		return ReasonOwnerGone
	case ownerErr != nil && r.missed >= r.opts.MissedProbes:
		return ReasonOwnerUnreachable
	}
	if age < r.opts.Grace || w.RunnerID == 0 || w.Repo == "" {
		return ""
	}
	runner, err := r.runners.Runner(ctx, w.InstallationID, w.Repo, w.RunnerID)
	switch {
	case err != nil:
		r.logger.Warn("runner lookup failed, worker kept", "container_id", shortID(w.ID), "err", err)
		return ""
	case !runner.Found:
		return ReasonRunnerGone
	case runner.Status == "offline":
		return ReasonRunnerOffline
	}
	return ""
}

// HealthOwner returns an Owner that reads the instance ID from the mayfly /healthz endpoint at url.
func HealthOwner(client *http.Client, url string) Owner {
	return func(ctx context.Context) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("healthz: status %d", resp.StatusCode)
		}
		var h struct {
			Instance string `json:"instance"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
			return "", fmt.Errorf("healthz: %w", err)
		}
		if h.Instance == "" {
			return "", errors.New("healthz: no instance in response")
		}
		return h.Instance, nil
	}
}

// shortID returns the 12-character form Docker shows for container IDs.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
