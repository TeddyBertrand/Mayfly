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
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
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
	queue   chan receiver.Job
	logger  *slog.Logger
	newName func(int64) (string, error)
}

// New returns a Provisioner with workers goroutines and room for queueSize waiting jobs.
// Runners register with the self-hosted defaults plus labels.
func New(jit JITIssuer, backend worker.Backend, labels []string, workers, queueSize int, logger *slog.Logger) *Provisioner {
	return &Provisioner{
		jit:     jit,
		backend: backend,
		labels:  runnerLabels(labels),
		workers: workers,
		queue:   make(chan receiver.Job, queueSize),
		logger:  logger,
		newName: github.RunnerName,
	}
}

// Enqueue adds job to the queue without blocking, returning ErrQueueFull when it is full.
func (p *Provisioner) Enqueue(job receiver.Job) error {
	select {
	case p.queue <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Run processes queued jobs until ctx is cancelled, then waits for in-flight workers to be cleaned up.
// Cancelling ctx stops running workers, so their jobs fail on GitHub.
func (p *Provisioner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range p.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-p.queue:
					// select picks randomly when both cases are ready, so re-check before provisioning.
					if ctx.Err() != nil {
						p.logger.Warn("queued job dropped at shutdown", "job_id", job.ID, "repo", job.Repo)
						return
					}
					p.handle(ctx, job)
				}
			}
		}()
	}
	p.logger.Info("provisioner started", "workers", p.workers, "queue_size", cap(p.queue), "labels", p.labels)
	wg.Wait()

	for {
		select {
		case job := <-p.queue:
			p.logger.Warn("queued job dropped at shutdown", "job_id", job.ID, "repo", job.Repo)
		default:
			p.logger.Info("provisioner stopped")
			return
		}
	}
}

// handle registers a JIT runner for job and runs a worker with it until the worker is gone.
func (p *Provisioner) handle(ctx context.Context, job receiver.Job) {
	log := p.logger.With("job_id", job.ID, "repo", job.Repo)
	start := time.Now()

	name, err := p.newName(job.ID)
	if err != nil {
		log.Error("runner name failed", "err", err)
		return
	}
	log = log.With("runner_name", name)

	jitCtx, cancel := context.WithTimeout(ctx, jitTimeout)
	cfg, err := p.jit.GenerateJITConfig(jitCtx, job.InstallationID, job.Repo, name, p.labels)
	cancel()
	if err != nil {
		log.Error("jit config failed", "err", err)
		return
	}
	log.Info("runner registered", "runner_id", cfg.RunnerID, "elapsed", time.Since(start).String())

	if err := p.backend.Run(ctx, worker.Spec{JobID: job.ID, RunnerName: name, JITConfig: cfg.Encoded}); err != nil {
		log.Error("worker failed", "err", err)
		return
	}
	log.Info("job done", "elapsed", time.Since(start).String())
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
