// Package reaper destroys leaked or stuck workers as a safety net.
package reaper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// Reasons a worker is removed, as logged and returned by Sweep.
const (
	ReasonMaxAge           = "max age exceeded"
	ReasonOwnerGone        = "owner instance gone"
	ReasonOwnerUnreachable = "owner unreachable"
	ReasonRunnerGone       = "runner registration gone"
	ReasonRunnerOffline    = "runner offline"

	// ReasonMayflyUnreachable and ReasonNotPickedUp explain a stuck job.
	ReasonMayflyUnreachable = "mayfly unreachable"
	ReasonNotPickedUp       = "not picked up: mayfly is up but no runner took the job, check its logs for this job"
)

// reasonLabels are the short reason label values on the reaper metrics, one per reason above.
var reasonLabels = map[string]string{
	ReasonMaxAge:            "max_age",
	ReasonOwnerGone:         "owner_gone",
	ReasonOwnerUnreachable:  "owner_unreachable",
	ReasonRunnerGone:        "runner_gone",
	ReasonRunnerOffline:     "runner_offline",
	ReasonMayflyUnreachable: "mayfly_unreachable",
	ReasonNotPickedUp:       "not_picked_up",
}

// presetReasons starts every reason series at 0, so increase() also counts the first removal or stuck job of each kind.
func presetReasons(m *metrics.Reaper) {
	for _, r := range []string{ReasonMaxAge, ReasonOwnerGone, ReasonOwnerUnreachable, ReasonRunnerGone, ReasonRunnerOffline} {
		metrics.Preset(m.WorkersRemoved, reasonLabels[r])
	}
	for _, r := range []string{ReasonMayflyUnreachable, ReasonNotPickedUp} {
		metrics.Preset(m.StuckJobs, reasonLabels[r])
	}
}

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

// sweepRegistrations deletes mayfly registrations that stayed offline, with no container claiming them,
// for StaleAfter. claimed holds the runner IDs of containers still alive after the container pass.
func (r *Reaper) sweepRegistrations(ctx context.Context, repos []github.Repo, claimed map[int64]bool) []github.RunnerRegistration {
	var deleted []github.RunnerRegistration
	seen := map[int64]bool{}
	for _, repo := range repos {
		regs, err := r.gh.Runners(ctx, repo.InstallationID, repo.FullName)
		if err != nil {
			r.logger.Warn("runner list failed, registrations kept", logkey.Repo, repo.FullName, logkey.Err, err)
			// Keep this repo's timers: a failed list says nothing about whether its runners recovered.
			for id := range r.staleSince {
				seen[id] = true
			}
			continue
		}
		for _, reg := range regs {
			// Only names the provisioner generates; other self-hosted runners on the repo are never touched.
			if !strings.HasPrefix(reg.Name, github.RunnerNamePrefix) || reg.Status != github.RunnerStatusOffline || claimed[reg.ID] {
				continue
			}
			seen[reg.ID] = true
			since, ok := r.staleSince[reg.ID]
			if !ok {
				r.staleSince[reg.ID] = r.now()
				continue
			}
			if r.now().Sub(since) < r.opts.StaleAfter {
				continue
			}
			log := r.logger.With(logkey.Repo, repo.FullName, logkey.RunnerID, reg.ID, logkey.RunnerName, reg.Name)
			if err := r.gh.DeleteRunner(ctx, repo.InstallationID, repo.FullName, reg.ID); err != nil {
				log.Error("stale registration delete failed", logkey.Err, err)
				continue
			}
			r.metrics.RegistrationsRemoved.Inc()
			log.Info("stale registration removed", logkey.StaleFor, r.now().Sub(since).Round(time.Second).String())
			delete(r.staleSince, reg.ID)
			deleted = append(deleted, reg)
		}
	}
	// A registration that came online, got a container or disappeared starts its window over if it goes stale again.
	for id := range r.staleSince {
		if !seen[id] {
			delete(r.staleSince, id)
		}
	}
	return deleted
}

// sweepStuck logs, once each, mayfly jobs queued for longer than StuckAfter, with a reason based on the owner probe.
func (r *Reaper) sweepStuck(ctx context.Context, repos []github.Repo, ownerErr error) []Stuck {
	var stuck []Stuck
	queued := map[int64]bool{}
	for _, repo := range repos {
		jobs, err := r.gh.QueuedJobs(ctx, repo.InstallationID, repo.FullName)
		if err != nil {
			r.logger.Warn("queued job list failed", logkey.Repo, repo.FullName, logkey.Err, err)
			for id := range r.reported {
				queued[id] = true
			}
			continue
		}
		for _, j := range jobs {
			if !workflow.MatchesLabels(j.Labels, r.opts.Labels) {
				continue
			}
			queued[j.ID] = true
			age := r.now().Sub(j.CreatedAt)
			if age < r.opts.StuckAfter || r.reported[j.ID] {
				continue
			}
			reason := ReasonNotPickedUp
			if ownerErr != nil {
				reason = ReasonMayflyUnreachable
			}
			r.reported[j.ID] = true
			r.metrics.StuckJobs.WithLabelValues(reasonLabels[reason]).Inc()
			r.logger.Warn("job stuck in queued", logkey.Repo, repo.FullName, logkey.JobID, j.ID, logkey.JobName, j.Name,
				logkey.Age, age.Round(time.Second).String(), logkey.Reason, reason, logkey.URL, j.URL)
			stuck = append(stuck, Stuck{Repo: repo.FullName, Job: j, Reason: reason})
		}
	}
	for id := range r.reported {
		if !queued[id] {
			delete(r.reported, id)
		}
	}
	return stuck
}

// reason returns why w should be removed, or "" to keep it. live and ownerErr are this cycle's owner probe.
func (r *Reaper) reason(ctx context.Context, w worker.Info, live string, ownerErr error) string {
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
	runner, err := r.gh.Runner(ctx, w.InstallationID, w.Repo, w.RunnerID)
	switch {
	case err != nil:
		r.logger.Warn("runner lookup failed, worker kept", logkey.ContainerID, worker.ShortID(w.ID), logkey.Err, err)
		return ""
	case !runner.Found:
		return ReasonRunnerGone
	case runner.Status == github.RunnerStatusOffline:
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
