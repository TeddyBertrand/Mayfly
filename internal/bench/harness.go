package bench

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

const (
	// runnerInput is the workflow_dispatch input of the demo workflows that picks the runner type.
	runnerInput = "runner"
	// ConclusionTimedOut marks a run the harness gave up on; it counts as failed.
	ConclusionTimedOut = "timed_out"
	// maxConsecutiveTimeouts stops the benchmark: one timeout is a GitHub glitch, several in a row a broken setup.
	maxConsecutiveTimeouts = 2
)

// errRunTimedOut is a run that did not complete within Config.RunTimeout.
var errRunTimedOut = errors.New("run timed out")

// API is the part of the GitHub API the harness uses; github.TokenClient implements it.
type API interface {
	DispatchWorkflow(ctx context.Context, repo, file, ref string, inputs map[string]string) (int64, error)
	Run(ctx context.Context, repo string, runID int64) (github.WorkflowRun, error)
	RunJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	CancelRun(ctx context.Context, repo string, runID int64) error
}

// Config describes one benchmark: which workflows run on which runner types, and how often.
type Config struct {
	Repo      string
	Ref       string
	Workflows []string
	Runners   []string
	Warmup    int
	Runs      int
	// Poll is how often a dispatched run is checked; RunTimeout gives up on one run.
	Poll       time.Duration
	RunTimeout time.Duration
}

// Harness dispatches the scheduled runs one at a time and records each job's timing.
type Harness struct {
	API    API
	Logger *slog.Logger
	Now    func() time.Time
	// Sleep waits d or until ctx ends; tests replace it to run without waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Run executes every item of cfg's schedule in order and passes each record to emit.
// Runs are sequential so jobs never compete for the same host. A run that times out is cancelled and recorded
// as timed_out; maxConsecutiveTimeouts in a row, or any other error, stops the benchmark.
func (h *Harness) Run(ctx context.Context, cfg Config, emit func(Record) error) error {
	items := Schedule(cfg.Workflows, cfg.Runners, cfg.Warmup, cfg.Runs)
	timeouts := 0
	for n, it := range items {
		log := h.Logger.With(logkey.Workflow, it.Workflow, logkey.Runner, it.Runner, logkey.Iteration, it.Iteration)
		rec, err := h.runOne(ctx, cfg, it)
		switch {
		case errors.Is(err, errRunTimedOut):
			timeouts++
			rec.Conclusion = ConclusionTimedOut
			// A run stuck before GitHub created its job cannot be cancelled (409); it expires on its own.
			if cerr := h.API.CancelRun(ctx, cfg.Repo, rec.RunID); cerr != nil {
				log.Warn("cancel timed-out run failed", logkey.RunID, rec.RunID, logkey.Err, cerr)
			}
			if timeouts >= maxConsecutiveTimeouts {
				_ = emit(rec)
				return fmt.Errorf("%d runs in a row timed out, last %s on %s: %w", timeouts, it.Workflow, it.Runner, err)
			}
		case err != nil:
			return fmt.Errorf("%s on %s, iteration %d: %w", it.Workflow, it.Runner, it.Iteration, err)
		default:
			timeouts = 0
		}
		if err := emit(rec); err != nil {
			return fmt.Errorf("record run %d: %w", rec.RunID, err)
		}
		log.Info("run recorded", logkey.RunID, rec.RunID, logkey.Conclusion, rec.Conclusion,
			logkey.Total, rec.Total, logkey.Progress, fmt.Sprintf("%d/%d", n+1, len(items)))
	}
	return nil
}

// runOne dispatches it, waits for the run to complete and measures its single job.
func (h *Harness) runOne(ctx context.Context, cfg Config, it Item) (Record, error) {
	rec := Record{DispatchedAt: h.Now(), Item: it}
	id, err := h.API.DispatchWorkflow(ctx, cfg.Repo, it.Workflow+".yml", cfg.Ref, map[string]string{runnerInput: it.Runner})
	if err != nil {
		return rec, err
	}
	rec.RunID = id

	ctx, cancel := context.WithTimeout(ctx, cfg.RunTimeout)
	defer cancel()
	run, err := h.waitRun(ctx, cfg, id)
	if err != nil {
		return rec, err
	}
	job, err := h.waitJob(ctx, cfg, id)
	if err != nil {
		return rec, err
	}
	rec.JobID, rec.RunnerName, rec.Conclusion = job.ID, job.RunnerName, run.Conclusion
	if run.Conclusion != ConclusionSuccess {
		return rec, nil
	}
	if rec.Timing, err = Measure(job); err != nil {
		return rec, err
	}
	return rec, nil
}

// waitRun polls run id until it completes or ctx's deadline passes.
func (h *Harness) waitRun(ctx context.Context, cfg Config, id int64) (github.WorkflowRun, error) {
	return poll(ctx, h, cfg, id, func() (github.WorkflowRun, bool, error) {
		run, err := h.API.Run(ctx, cfg.Repo, id)
		return run, run.Status == github.RunStatusCompleted, err
	})
}

// waitJob polls the jobs of run id until its single job reads completed too.
// The runs and jobs endpoints are updated separately, so a completed run can still list its job as in progress.
func (h *Harness) waitJob(ctx context.Context, cfg Config, id int64) (github.Job, error) {
	return poll(ctx, h, cfg, id, func() (github.Job, bool, error) {
		jobs, err := h.API.RunJobs(ctx, cfg.Repo, id)
		if err != nil {
			return github.Job{}, false, err
		}
		if len(jobs) != 1 {
			return github.Job{}, false, fmt.Errorf("run %d has %d jobs, want 1", id, len(jobs))
		}
		return jobs[0], jobs[0].Status == github.RunStatusCompleted, nil
	})
}

// poll calls check every cfg.Poll until it reports done or ctx's deadline passes. A failed check is logged and retried.
func poll[T any](ctx context.Context, h *Harness, cfg Config, id int64, check func() (T, bool, error)) (T, error) {
	var zero T
	for {
		if err := h.Sleep(ctx, cfg.Poll); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return zero, fmt.Errorf("run %d not completed after %s: %w", id, cfg.RunTimeout, errRunTimedOut)
			}
			return zero, err
		}
		v, done, err := check()
		if err != nil {
			h.Logger.Warn("poll failed, retrying", logkey.RunID, id, logkey.Err, err)
			continue
		}
		if done {
			return v, nil
		}
	}
}

// SleepContext waits d, or returns ctx's error if it ends first.
func SleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
