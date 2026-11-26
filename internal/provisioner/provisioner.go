// Package provisioner creates ephemeral workers and registers them as just-in-time runners.
package provisioner

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/retry"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// ErrQueueFull is returned by Enqueue when no queue slot is free.
var ErrQueueFull = errors.New("provisioner queue full")

// jitTimeout bounds the GitHub call so a slow API cannot hold a worker slot.
const jitTimeout = 30 * time.Second

// baseRunnerLabels are the default labels of a self-hosted Linux x64 runner.
var baseRunnerLabels = []string{"self-hosted", "linux", "x64"}

// JITIssuer registers a just-in-time runner and returns its one-shot config.
type JITIssuer interface {
	GenerateJITConfig(ctx context.Context, installationID int64, repo, name string, labels []string) (github.JITConfig, error)
}

// Provisioner turns queued jobs into running workers through a bounded queue and a fixed pool.
type Provisioner struct {
	jit     JITIssuer
	backend worker.Backend
	labels  []string
	workers int
	queue   chan workflow.Job
	jobs    *tracker
	logger  *slog.Logger
	newName func(int64) (string, error)
	retry   retry.Policy
	metrics *metrics.Mayfly
	now     func() time.Time
	// pool is nil when no repository keeps warm runners.
	pool *pool
}

// Options configure a Provisioner.
type Options struct {
	// Labels are added to the self-hosted defaults every runner registers with.
	Labels []string
	// Workers is how many jobs are provisioned at once.
	Workers int
	// QueueSize is how many jobs may wait for a free worker.
	QueueSize int
	// Metrics receives job and worker metrics; nil uses unregistered ones.
	Metrics *metrics.Mayfly
	// Pool keeps warm runners; its zero value disables it.
	Pool PoolOptions
}

// New returns a Provisioner that registers runners through jit and runs them on backend, as configured by opts.
func New(jit JITIssuer, backend worker.Backend, opts Options, logger *slog.Logger) *Provisioner {
	m := opts.Metrics
	if m == nil {
		m = metrics.NewMayfly(nil)
	}
	p := &Provisioner{
		jit:     jit,
		backend: backend,
		labels:  runnerLabels(opts.Labels),
		workers: opts.Workers,
		queue:   make(chan workflow.Job, opts.QueueSize),
		jobs:    newTracker(),
		logger:  logger,
		newName: github.RunnerName,
		retry:   retry.Default,
		metrics: m,
		now:     time.Now,
	}
	p.pool = newPool(p, opts.Pool)
	if p.pool != nil {
		m.RegisterPool(p.pool.snapshot)
	}
	return p
}

// Enqueue hands job to a free warm runner of its repository, or else tracks it as waiting and adds it
// to the queue without blocking, returning ErrQueueFull when it is full. A job already tracked is ignored.
func (p *Provisioner) Enqueue(job workflow.Job) error {
	if _, tracked := p.jobs.state(job.ID); !tracked {
		reserved, pooled := p.pool.reserve(job)
		if reserved {
			p.logger.Info("job reserved a warm runner", logkey.JobID, job.ID, logkey.Repo, job.Repo)
			return nil
		}
		if pooled {
			p.logger.Info("no warm runner free, cold start", logkey.JobID, job.ID, logkey.Repo, job.Repo)
		}
	}
	if !p.jobs.add(job.ID, p.now()) {
		p.logger.Info("job already tracked, queued event ignored", logkey.JobID, job.ID, logkey.Repo, job.Repo)
		return nil
	}
	select {
	case p.queue <- job:
		p.metrics.JobsWaiting.Inc()
		return nil
	default:
		p.jobs.remove(job.ID)
		return ErrQueueFull
	}
}

// Update applies an in_progress or completed event to the tracked job it concerns, without blocking.
// Cancelling a job that has not started stops its worker; events for untracked jobs are ignored.
func (p *Provisioner) Update(action workflow.Action, job workflow.Job) {
	log := p.logger.With(logkey.Action, action, logkey.JobID, job.ID, logkey.Repo, job.Repo, logkey.RunnerName, job.RunnerName)
	p.observe(action, job)
	if warm, reservedAt := p.pool.update(action, job); warm {
		if !reservedAt.IsZero() {
			p.metrics.PoolClaimSeconds.Observe(p.now().Sub(reservedAt).Seconds())
		}
		// A cold worker started for this job before the pool could serve it would otherwise idle until its timeout.
		if c, err := p.jobs.drop(job.ID); err == nil {
			log.Info("warm runner took the job, cold worker dropped", logkey.From, c.From.String())
			return
		}
		log.Info("warm runner took the job")
		return
	}
	c, err := p.jobs.update(action, job)
	switch {
	case errors.Is(err, errUntracked):
		log.Info("update for untracked job ignored")
	case err != nil:
		log.Warn("job update rejected", logkey.TrackedJobID, c.ID, logkey.Err, err)
	default:
		if c.To == Running {
			p.metrics.BootSeconds.Observe(p.now().Sub(c.Accepted).Seconds())
		}
		log.Info("job state changed", logkey.TrackedJobID, c.ID, logkey.From, c.From.String(), logkey.To, c.To.String())
	}
}

// Run processes queued jobs and keeps the warm pool full until ctx is cancelled, then waits for
// in-flight workers to be cleaned up. Cancelling ctx stops running workers, so their jobs fail on GitHub.
func (p *Provisioner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	if p.pool != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.pool.run(ctx)
		}()
	}
	for range p.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-p.queue:
					p.metrics.JobsWaiting.Dec()
					// select picks randomly when both cases are ready, so re-check before provisioning.
					if ctx.Err() != nil {
						p.logger.Warn("queued job dropped at shutdown", logkey.JobID, job.ID, logkey.Repo, job.Repo)
						return
					}
					p.handle(ctx, job)
				}
			}
		}()
	}
	p.logger.Info("provisioner started", logkey.Workers, p.workers, logkey.QueueSize, cap(p.queue), logkey.Labels, p.labels)
	wg.Wait()

	for {
		select {
		case job := <-p.queue:
			p.metrics.JobsWaiting.Dec()
			p.logger.Warn("queued job dropped at shutdown", logkey.JobID, job.ID, logkey.Repo, job.Repo)
		default:
			p.logger.Info("provisioner stopped")
			return
		}
	}
}

// handle registers a JIT runner for job and runs a worker with it until the worker is gone.
// It stops early if the job is dropped, and stops tracking the job when it returns.
func (p *Provisioner) handle(ctx context.Context, job workflow.Job) {
	log := p.logger.With(logkey.JobID, job.ID, logkey.Repo, job.Repo)
	start := time.Now()
	defer p.jobs.remove(job.ID)

	// Each worker gets its own context so a cancelled job stops only its worker.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := p.jobs.begin(job.ID, cancel); err != nil {
		log.Info("job dropped before start, skipped", logkey.Reason, err)
		return
	}

	name, cfg, err := p.register(wctx, job.InstallationID, job.Repo, func() (string, error) { return p.newName(job.ID) }, log)
	if err != nil {
		if p.dropped(ctx, wctx) {
			log.Info("job dropped while registering runner")
			return
		}
		p.metrics.ProvisionFailures.WithLabelValues(metrics.StageJITConfig).Inc()
		log.Error("jit config failed", logkey.Err, err)
		return
	}
	log = log.With(logkey.RunnerName, name)
	log.Info("runner registered", logkey.RunnerID, cfg.RunnerID, logkey.Elapsed, time.Since(start).String())

	if err := p.jobs.ready(job.ID, name); err != nil {
		// The registration is left on GitHub; the reaper removes stale runners.
		log.Info("job dropped after runner registration, worker not started", logkey.Reason, err)
		return
	}

	p.metrics.WorkersAlive.Inc()
	err = p.backend.Run(wctx, worker.Spec{
		JobID: job.ID, RunnerName: name, JITConfig: cfg.Encoded,
		Repo: job.Repo, InstallationID: job.InstallationID, RunnerID: cfg.RunnerID,
	})
	p.metrics.WorkersAlive.Dec()
	if err != nil {
		if p.dropped(ctx, wctx) {
			log.Info("worker stopped, job cancelled before it started")
			return
		}
		p.metrics.ProvisionFailures.WithLabelValues(metrics.StageWorker).Inc()
		log.Error("worker failed", logkey.Err, err)
		return
	}
	log.Info("job done", logkey.Elapsed, time.Since(start).String())
}

// register registers a JIT runner on repo, named by newName, retrying temporary failures.
// It returns the runner's name and its one-shot config.
func (p *Provisioner) register(ctx context.Context, installationID int64, repo string, newName func() (string, error), log *slog.Logger) (string, github.JITConfig, error) {
	var name string
	var cfg github.JITConfig
	err := retry.Do(ctx, p.retry, func(ctx context.Context) error {
		// A fresh name per attempt: if a lost response hid a successful registration, reusing the name gets a 409.
		n, err := newName()
		if err != nil {
			return err
		}
		jitCtx, jitCancel := context.WithTimeout(ctx, jitTimeout)
		defer jitCancel()
		c, err := p.jit.GenerateJITConfig(jitCtx, installationID, repo, n, p.labels)
		if err != nil {
			return err
		}
		name, cfg = n, c
		return nil
	}, func(attempt int, wait time.Duration, err error) {
		p.metrics.Retries.WithLabelValues(metrics.OpJITConfig).Inc()
		log.Warn("jit config failed, retrying", logkey.Attempt, attempt, logkey.Wait, wait.String(), logkey.Err, err)
	})
	return name, cfg, err
}

// dropped reports whether wctx was cancelled by a job update rather than by shutdown of ctx.
func (p *Provisioner) dropped(ctx, wctx context.Context) bool {
	return wctx.Err() != nil && ctx.Err() == nil
}

// runnerLabels returns the base labels followed by extra, without case-insensitive duplicates.
func runnerLabels(extra []string) []string {
	out := make([]string, 0, len(baseRunnerLabels)+len(extra))
	seen := map[string]bool{}
	for _, l := range append(append([]string{}, baseRunnerLabels...), extra...) {
		key := strings.ToLower(l)
		if !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
	}
	return out
}
