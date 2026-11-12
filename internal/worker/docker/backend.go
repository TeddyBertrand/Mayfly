package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

const (
	// DefaultSocket is where the Docker daemon listens on Linux.
	DefaultSocket = "/var/run/docker.sock"
	// DefaultImage is the official runner image, pinned to an exact release.
	DefaultImage = "ghcr.io/actions/actions-runner:2.338.2"
	// DefaultTimeout matches GitHub's maximum job duration.
	DefaultTimeout = 6 * time.Hour

	// LabelJobID and LabelRunnerName tag every worker container so the reaper can find them.
	LabelJobID      = "mayfly.job_id"
	LabelRunnerName = "mayfly.runner_name"

	removeTimeout = 30 * time.Second
)

// Backend runs each worker as a Docker container that is removed when its job ends.
type Backend struct {
	eng     *engine
	image   string
	cmd     []string
	timeout time.Duration
	logger  *slog.Logger
}

var _ worker.Backend = (*Backend)(nil)

// New returns a Backend using the daemon on socket, running image, and killing workers after timeout.
func New(socket, image string, timeout time.Duration, logger *slog.Logger) *Backend {
	return &Backend{eng: newSocketEngine(socket), image: image, cmd: []string{"./run.sh"}, timeout: timeout, logger: logger}
}

// Run starts a runner container for spec, waits for it to finish, and always removes it.
func (b *Backend) Run(ctx context.Context, spec worker.Spec) error {
	log := b.logger.With("job_id", spec.JobID, "runner_name", spec.RunnerName, "image", b.image)
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	if err := b.ensureImage(ctx, log); err != nil {
		log.Error("worker image unavailable", "err", err)
		return err
	}

	id, err := b.eng.createContainer(ctx, spec.RunnerName, b.containerSpec(spec))
	if err != nil {
		log.Error("worker create failed", "err", err)
		return err
	}
	log = log.With("container_id", shortID(id))
	defer b.remove(id, log)

	waitDone, err := b.eng.waitRemoved(ctx, id)
	if err != nil {
		log.Error("worker wait failed", "err", err)
		return err
	}
	if err := b.eng.startContainer(ctx, id); err != nil {
		log.Error("worker start failed", "err", err)
		return err
	}
	log.Info("worker started")

	exit, err := waitDone()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn("worker timed out", "timeout", b.timeout.String())
			return fmt.Errorf("worker timed out after %s: %w", b.timeout, ctx.Err())
		}
		log.Error("worker wait failed", "err", err)
		return err
	}
	// The runner exits 0 even when it fails; the job result is reported to GitHub, not here.
	log.Info("worker finished", "exit_code", exit)
	return nil
}

// containerSpec builds the create request for spec. The JIT config goes in the environment, not argv,
// so it does not show up in the host's process list.
func (b *Backend) containerSpec(spec worker.Spec) containerSpec {
	cs := containerSpec{
		Image: b.image,
		Cmd:   b.cmd,
		Env:   []string{"ACTIONS_RUNNER_INPUT_JITCONFIG=" + spec.JITConfig},
		Labels: map[string]string{
			LabelJobID:      strconv.FormatInt(spec.JobID, 10),
			LabelRunnerName: spec.RunnerName,
		},
	}
	cs.HostConfig.AutoRemove = true
	return cs
}

// ensureImage pulls the image when it is not present locally.
func (b *Backend) ensureImage(ctx context.Context, log *slog.Logger) error {
	ok, err := b.eng.imageExists(ctx, b.image)
	if err != nil || ok {
		return err
	}
	log.Info("pulling worker image")
	return b.eng.pullImage(ctx, b.image)
}

// remove force-removes the container with its own deadline, since the job context may be done.
func (b *Backend) remove(id string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), removeTimeout)
	defer cancel()
	if err := b.eng.removeContainer(ctx, id); err != nil {
		log.Error("worker remove failed, left for the reaper", "err", err)
		return
	}
	log.Info("worker removed")
}

// shortID returns the 12-character form Docker shows for container IDs.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
