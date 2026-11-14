package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/retry"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

const (
	// LabelJobID and LabelRunnerName tag every worker container so the reaper can find them.
	LabelJobID      = "mayfly.job_id"
	LabelRunnerName = "mayfly.runner_name"
	// LabelOwner is the instance ID of the mayfly process that started the worker.
	LabelOwner = "mayfly.owner"
	// LabelRepo, LabelInstallationID and LabelRunnerID identify the runner registration on GitHub.
	LabelRepo           = "mayfly.repo"
	LabelInstallationID = "mayfly.installation_id"
	LabelRunnerID       = "mayfly.runner_id"

	removeTimeout = 30 * time.Second
)

// Backend runs each worker as a Docker container that is removed when its job ends.
type Backend struct {
	eng     *engine
	image   string
	network string
	cmd     []string
	timeout time.Duration
	logger  *slog.Logger
	retry   retry.Policy
	owner   string
}

var _ worker.Backend = (*Backend)(nil)

// New returns a Backend using the daemon on socket, running image on network (empty for Docker's default),
// and killing workers after timeout. Every container is labelled with owner, the instance ID of this
// process, so the reaper can spot orphans.
func New(socket, image, network string, timeout time.Duration, owner string, logger *slog.Logger) *Backend {
	return &Backend{eng: newSocketEngine(socket), image: image, network: network, cmd: []string{"./run.sh"}, timeout: timeout, logger: logger, retry: retry.Default, owner: owner}
}

// Run starts a runner container for spec, waits for it to finish, and always removes it.
func (b *Backend) Run(ctx context.Context, spec worker.Spec) error {
	log := b.logger.With("job_id", spec.JobID, "runner_name", spec.RunnerName, "image", b.image)
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	var id string
	var waitDone func() (int, error)
	var stopWait context.CancelFunc
	err := retry.Do(ctx, b.retry, func(ctx context.Context) error {
		var err error
		id, waitDone, stopWait, err = b.start(ctx, spec, log)
		return err
	}, func(attempt int, wait time.Duration, err error) {
		log.Warn("worker start failed, retrying", "attempt", attempt, "wait", wait.String(), "err", err)
	})
	if err != nil {
		return err
	}
	log = log.With("container_id", shortID(id))
	defer stopWait()
	defer b.remove(id, log)
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
			LabelJobID:          strconv.FormatInt(spec.JobID, 10),
			LabelRunnerName:     spec.RunnerName,
			LabelOwner:          b.owner,
			LabelRepo:           spec.Repo,
			LabelInstallationID: strconv.FormatInt(spec.InstallationID, 10),
			LabelRunnerID:       strconv.FormatInt(spec.RunnerID, 10),
		},
	}
	cs.HostConfig.AutoRemove = true
	cs.HostConfig.NetworkMode = b.network
	return cs
}

// start makes sure the image is present, then creates and starts the container, as one retryable attempt.
// On success it returns the container ID, the wait for its removal, and the function that ends that wait.
// On failure it leaves nothing behind, so the next attempt starts clean.
func (b *Backend) start(ctx context.Context, spec worker.Spec, log *slog.Logger) (string, func() (int, error), context.CancelFunc, error) {
	if err := b.ensureImage(ctx, log); err != nil {
		log.Error("worker image unavailable", "err", err)
		return "", nil, nil, err
	}
	if err := b.ensureNetwork(ctx, log); err != nil {
		log.Error("worker network unavailable", "err", err, "network", b.network)
		return "", nil, nil, err
	}

	id, err := b.eng.createContainer(ctx, spec.RunnerName, b.containerSpec(spec))
	if err != nil {
		log.Error("worker create failed", "err", err)
		return "", nil, nil, err
	}

	// The wait request gets its own context so a failed attempt can close it instead of leaking the connection.
	wctx, stopWait := context.WithCancel(ctx)
	fail := func(msg string, err error) (string, func() (int, error), context.CancelFunc, error) {
		log.Error(msg, "err", err, "container_id", shortID(id))
		stopWait()
		b.remove(id, log)
		return "", nil, nil, err
	}
	waitDone, err := b.eng.waitRemoved(wctx, id)
	if err != nil {
		return fail("worker wait failed", err)
	}
	if err := b.eng.startContainer(ctx, id); err != nil {
		return fail("worker start failed", err)
	}
	return id, waitDone, stopWait, nil
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

// ensureNetwork creates the worker network when one is configured and missing. The bridge interface
// gets the network's name, so host firewall rules can match it.
func (b *Backend) ensureNetwork(ctx context.Context, log *slog.Logger) error {
	if b.network == "" {
		return nil
	}
	ok, err := b.eng.networkExists(ctx, b.network)
	if err != nil || ok {
		return err
	}
	log.Info("creating worker network", "network", b.network)
	return b.eng.createNetwork(ctx, b.network)
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
