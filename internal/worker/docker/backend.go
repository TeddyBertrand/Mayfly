package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/retry"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

const (
	// runnerCommand starts the runner in the official image; with a JIT config it takes one job and exits.
	runnerCommand = "./run.sh"
	// envJITConfig is the variable the runner reads its JIT config from, which keeps it out of the process list.
	envJITConfig = "ACTIONS_RUNNER_INPUT_JITCONFIG"
	// envResultsURL points actions/cache at the cache server; only the patched runner keeps it (decision 028).
	envResultsURL = "ACTIONS_RESULTS_URL"
	// envGoProxy and envNPMRegistry point Go and npm at the package proxies (decision 030).
	envGoProxy     = "GOPROXY"
	envNPMRegistry = "npm_config_registry"
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
	eng      *engine
	image    string
	network  string
	cacheURL string
	tmpfs    int64
	proxyEnv []string
	cmd      []string
	timeout  time.Duration
	logger   *slog.Logger
	retry    retry.Policy
	owner    string
	retries  prometheus.Counter
}

var _ worker.Backend = (*Backend)(nil)

// Options configure a Backend.
type Options struct {
	// Socket is the path of the Docker daemon socket.
	Socket string
	// Image is the runner image every worker runs.
	Image string
	// Network is the Docker network workers join; empty means Docker's default.
	Network string
	// CacheURL is the cache server workers use for actions/cache; empty keeps GitHub's.
	CacheURL string
	// TmpfsBytes is the memory budget of the worker's tmpfs mounts; 0 keeps them on disk.
	TmpfsBytes int64
	// GoProxyURL and NPMRegistryURL are the package proxies workers use; empty keeps the public ones.
	GoProxyURL     string
	NPMRegistryURL string
	// Timeout is how long a worker may run before it is killed.
	Timeout time.Duration
	// Owner is the instance ID of this process, set on every container so the reaper can spot orphans.
	Owner string
	// Metrics receives worker start retries; nil uses unregistered ones.
	Metrics *metrics.Mayfly
}

// New returns a Backend that runs workers as configured by opts.
func New(opts Options, logger *slog.Logger) *Backend {
	m := opts.Metrics
	if m == nil {
		m = metrics.NewMayfly(nil)
	}
	return &Backend{
		eng: newSocketEngine(opts.Socket), image: opts.Image, network: opts.Network, cacheURL: opts.CacheURL, tmpfs: opts.TmpfsBytes, proxyEnv: proxyEnv(opts), cmd: []string{runnerCommand},
		timeout: opts.Timeout, logger: logger, retry: retry.Default, owner: opts.Owner,
		retries: m.Retries.WithLabelValues(metrics.OpWorkerStart),
	}
}

// proxyEnv returns the variables that point Go and npm at the configured package proxies.
func proxyEnv(opts Options) []string {
	var env []string
	if opts.GoProxyURL != "" {
		env = append(env, envGoProxy+"="+opts.GoProxyURL)
	}
	if opts.NPMRegistryURL != "" {
		env = append(env, envNPMRegistry+"="+opts.NPMRegistryURL)
	}
	return env
}

// Run starts a runner container for spec, waits for it to finish, and always removes it.
func (b *Backend) Run(ctx context.Context, spec worker.Spec) error {
	log := b.logger.With(logkey.JobID, spec.JobID, logkey.RunnerName, spec.RunnerName, logkey.Image, b.image)
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
		b.retries.Inc()
		log.Warn("worker start failed, retrying", logkey.Attempt, attempt, logkey.Wait, wait.String(), logkey.Err, err)
	})
	if err != nil {
		return err
	}
	log = log.With(logkey.ContainerID, worker.ShortID(id))
	defer stopWait()
	defer b.remove(id, log)
	log.Info("worker started")

	exit, err := waitDone()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn("worker timed out", logkey.Timeout, b.timeout.String())
			return fmt.Errorf("worker timed out after %s: %w", b.timeout, ctx.Err())
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			// The caller asked for the stop (retirement, cancelled job or shutdown), so nothing failed.
			log.Info("worker stopped before it exited")
			return err
		}
		log.Error("worker wait failed", logkey.Err, err)
		return err
	}
	// The runner exits 0 even when it fails; the job result is reported to GitHub, not here.
	log.Info("worker finished", logkey.ExitCode, exit)
	return nil
}

// containerSpec builds the create request for spec. The JIT config goes in the environment, not argv,
// so it does not show up in the host's process list.
func (b *Backend) containerSpec(spec worker.Spec) containerSpec {
	cs := containerSpec{
		Image: b.image,
		Cmd:   b.cmd,
		Env:   []string{envJITConfig + "=" + spec.JITConfig},
		Labels: map[string]string{
			LabelJobID:          strconv.FormatInt(spec.JobID, 10),
			LabelRunnerName:     spec.RunnerName,
			LabelOwner:          b.owner,
			LabelRepo:           spec.Repo,
			LabelInstallationID: strconv.FormatInt(spec.InstallationID, 10),
			LabelRunnerID:       strconv.FormatInt(spec.RunnerID, 10),
		},
	}
	if b.cacheURL != "" {
		cs.Env = append(cs.Env, envResultsURL+"="+b.cacheURL)
	}
	var tmpfsEnv []string
	cs.HostConfig.Tmpfs, tmpfsEnv = tmpfsSpec(b.tmpfs)
	cs.Env = append(cs.Env, tmpfsEnv...)
	cs.Env = append(cs.Env, b.proxyEnv...)
	cs.HostConfig.AutoRemove = true
	cs.HostConfig.NetworkMode = b.network
	return cs
}

// start makes sure the image is present, then creates and starts the container, as one retryable attempt.
// On success it returns the container ID, the wait for its removal, and the function that ends that wait.
// On failure it leaves nothing behind, so the next attempt starts clean.
func (b *Backend) start(ctx context.Context, spec worker.Spec, log *slog.Logger) (string, func() (int, error), context.CancelFunc, error) {
	if err := b.ensureImage(ctx, log); err != nil {
		log.Error("worker image unavailable", logkey.Err, err)
		return "", nil, nil, err
	}
	if err := b.ensureNetwork(ctx, log); err != nil {
		log.Error("worker network unavailable", logkey.Err, err, logkey.Network, b.network)
		return "", nil, nil, err
	}

	id, err := b.eng.createContainer(ctx, spec.RunnerName, b.containerSpec(spec))
	if err != nil {
		log.Error("worker create failed", logkey.Err, err)
		return "", nil, nil, err
	}

	// The wait request gets its own context so a failed attempt can close it instead of leaking the connection.
	wctx, stopWait := context.WithCancel(ctx)
	fail := func(msg string, err error) (string, func() (int, error), context.CancelFunc, error) {
		log.Error(msg, logkey.Err, err, logkey.ContainerID, worker.ShortID(id))
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
	log.Info("creating worker network", logkey.Network, b.network)
	return b.eng.createNetwork(ctx, b.network)
}

// remove force-removes the container with its own deadline, since the job context may be done.
func (b *Backend) remove(id string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), removeTimeout)
	defer cancel()
	if err := b.eng.removeContainer(ctx, id); err != nil {
		log.Error("worker remove failed, left for the reaper", logkey.Err, err)
		return
	}
	log.Info("worker removed")
}
