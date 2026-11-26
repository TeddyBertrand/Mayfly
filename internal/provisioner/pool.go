package provisioner

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

const (
	// reserveTimeout releases a reservation whose job never reached a warm runner, so the count cannot drift for good.
	reserveTimeout = 5 * time.Minute
	// sweepInterval is how often the pool releases expired reservations and tops itself up.
	sweepInterval = 30 * time.Second
	// warmRetryDelay spaces attempts after a warm runner or the installation lookup failed, so a broken setup does not spin.
	warmRetryDelay = 30 * time.Second
)

// RepoLister lists the repositories the App is installed on, with their installation IDs.
type RepoLister interface {
	Repos(ctx context.Context) ([]github.Repo, error)
}

// RunnerRemover deletes a runner registration, which GitHub refuses while the runner is busy.
type RunnerRemover interface {
	DeleteRunner(ctx context.Context, installationID int64, repo string, runnerID int64) error
}

// PoolOptions configure the warm pool (decisions 025 and 026). No repositories means no pool.
type PoolOptions struct {
	// Repos are the "owner/name" repositories that keep warm runners.
	Repos []string
	// Size is how many idle warm runners each repository keeps.
	Size int
	// Max caps the warm runners alive per repository, idle and busy; below Size it is raised to Size.
	Max int
	// IdleTimeout retires a warm runner that waited that long for a job; zero never retires.
	IdleTimeout time.Duration
	// Installations finds each repository's installation ID, which a JIT registration needs.
	Installations RepoLister
	// Remover deletes an idle runner's registration before it is retired.
	Remover RunnerRemover
}

// warmRepo is the pool state of one repository. A live runner is in exactly one of idle, retiring and busy.
type warmRepo struct {
	name string
	// installationID is 0 until the installation lookup found the repository.
	installationID int64
	// pending counts slots with no idle runner yet: one being registered, or one held through a back-off.
	pending int
	// idle holds the names of registered runners that have not taken a job.
	idle map[string]bool
	// retiring holds idle runners whose registration is being deleted; they no longer count as free.
	retiring map[string]bool
	// busy holds warm runners running a job.
	busy map[string]bool
	// reserved holds the queued jobs counting on an idle runner, with when they reserved it.
	reserved map[int64]time.Time
}

// available returns how many warm runners are free for a new job.
func (r *warmRepo) available() int {
	return r.pending + len(r.idle) - len(r.reserved)
}

// alive returns how many warm runners exist or are being started, which the pool's cap bounds.
func (r *warmRepo) alive() int {
	return r.pending + len(r.idle) + len(r.retiring) + len(r.busy)
}

// warmRunner is one registered warm runner.
type warmRunner struct {
	repo     *warmRepo
	runnerID int64
	// idleSince is when it was registered; it stops mattering once it takes a job.
	idleSince time.Time
	// stop cancels the runner's context, which removes its container.
	stop context.CancelFunc
}

// pool keeps idle, registered runners per repository and refills them as jobs take them, within a cap.
// GitHub picks which runner gets a job, so the pool only accounts for that choice through reservations.
type pool struct {
	p       *Provisioner
	size    int
	max     int
	list    RepoLister
	remover RunnerRemover
	wake    chan struct{}

	idleTimeout    time.Duration
	reserveTimeout time.Duration
	sweepInterval  time.Duration
	retryDelay     time.Duration
	newName        func() (string, error)

	mu    sync.Mutex
	repos map[string]*warmRepo
	// warm maps the name of every registered warm runner to its state.
	warm map[string]*warmRunner
}

// newPool returns the warm pool for p as configured by opts, or nil when no repository is pooled.
func newPool(p *Provisioner, opts PoolOptions) *pool {
	if len(opts.Repos) == 0 {
		return nil
	}
	w := &pool{
		p: p, size: opts.Size, max: max(opts.Max, opts.Size), list: opts.Installations, remover: opts.Remover,
		wake: make(chan struct{}, 1), idleTimeout: opts.IdleTimeout,
		reserveTimeout: reserveTimeout, sweepInterval: sweepInterval, retryDelay: warmRetryDelay,
		newName: github.WarmRunnerName,
		repos:   map[string]*warmRepo{}, warm: map[string]*warmRunner{},
	}
	for _, name := range opts.Repos {
		w.repos[repoKey(name)] = &warmRepo{
			name: name, idle: map[string]bool{}, retiring: map[string]bool{}, busy: map[string]bool{}, reserved: map[int64]time.Time{},
		}
		p.metrics.PresetPool(name)
	}
	return w
}

// snapshot returns the size of each repository's pool, for the pool gauges.
func (w *pool) snapshot() []metrics.PoolRepo {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]metrics.PoolRepo, 0, len(w.repos))
	for _, r := range w.repos {
		out = append(out, metrics.PoolRepo{
			Repo: r.name, Pending: r.pending, Idle: len(r.idle), Busy: len(r.busy), Retiring: len(r.retiring), Max: w.max,
		})
	}
	return out
}

// miss returns why r has no free runner, as a result label value. Callers hold mu.
func (w *pool) miss(r *warmRepo) string {
	switch {
	case r.installationID == 0:
		return metrics.PoolMissUnresolved
	case r.alive() >= w.max:
		return metrics.PoolMissMax
	default:
		return metrics.PoolMissEmpty
	}
}

// repoKey returns the map key of a repository, since GitHub names are case-insensitive.
func repoKey(repo string) string {
	return strings.ToLower(repo)
}

// reserve counts job on a free warm runner of its repository. reserved reports whether it did;
// pooled reports whether the repository has a pool at all, so a false reserved there is a pool miss.
func (w *pool) reserve(job workflow.Job) (reserved, pooled bool) {
	if w == nil {
		return false, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.repos[repoKey(job.Repo)]
	if r == nil {
		return false, false
	}
	if _, ok := r.reserved[job.ID]; ok {
		return true, true
	}
	if r.installationID == 0 || r.available() <= 0 {
		w.p.metrics.PoolRequests.WithLabelValues(r.name, w.miss(r)).Inc()
		return false, true
	}
	w.p.metrics.PoolRequests.WithLabelValues(r.name, metrics.PoolHit).Inc()
	r.reserved[job.ID] = w.p.now()
	w.signal()
	return true, true
}

// update applies an in_progress or completed event to the pool. warm reports an in_progress event on a
// warm runner; reservedAt is when the job reserved a warm runner, zero when it did not.
func (w *pool) update(action workflow.Action, job workflow.Job) (warm bool, reservedAt time.Time) {
	if w == nil {
		return false, time.Time{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if rn := w.warm[job.RunnerName]; rn != nil && action == workflow.ActionInProgress {
		warm = true
		r := rn.repo
		// A runner being retired can still win a job; then its registration delete fails and it keeps running.
		if r.idle[job.RunnerName] || r.retiring[job.RunnerName] {
			delete(r.idle, job.RunnerName)
			delete(r.retiring, job.RunnerName)
			r.busy[job.RunnerName] = true
			if _, ok := r.reserved[job.ID]; !ok {
				// A job the pool did not expect took the runner, so nothing refilled for it yet.
				w.signal()
			}
		}
	}
	// The job either took a runner or ended without one: its reservation has served its purpose.
	if r := w.repos[repoKey(job.Repo)]; r != nil {
		if at, ok := r.reserved[job.ID]; ok {
			reservedAt = at
			delete(r.reserved, job.ID)
		}
	}
	return warm, reservedAt
}

// signal wakes the pool loop without blocking; one pending wake-up is enough.
func (w *pool) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// run finds the pooled repositories' installations, then keeps each pool full until ctx is cancelled.
// It returns once every warm runner it started is gone.
func (w *pool) run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	if !w.resolve(ctx) {
		return
	}
	w.p.logger.Info("warm pool started", logkey.PoolSize, w.size, logkey.PoolMax, w.max,
		logkey.IdleTimeout, w.idleTimeout.String(), logkey.Repos, w.names())
	tick := time.NewTicker(w.sweepInterval)
	defer tick.Stop()
	for {
		w.expire()
		w.retireIdle(ctx, &wg)
		w.fill(ctx, &wg)
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-tick.C:
		}
	}
}

// names returns the configured names of the repositories that have an installation.
func (w *pool) names() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, r := range w.repos {
		if r.installationID != 0 {
			out = append(out, r.name)
		}
	}
	return out
}

// resolve looks up the installation of every pooled repository, retrying until it succeeds.
// It reports false if ctx was cancelled first. A repository the App is not installed on stays unpooled.
func (w *pool) resolve(ctx context.Context) bool {
	for {
		repos, err := w.list.Repos(ctx)
		if err == nil {
			w.setInstallations(repos)
			return true
		}
		w.p.logger.Warn("warm pool installation lookup failed, retrying", logkey.Err, err, logkey.Wait, w.retryDelay.String())
		if !sleep(ctx, w.retryDelay) {
			return false
		}
	}
}

// setInstallations records the installation ID of each pooled repository found in repos.
func (w *pool) setInstallations(repos []github.Repo) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, repo := range repos {
		if r := w.repos[repoKey(repo.FullName)]; r != nil {
			r.installationID = repo.InstallationID
		}
	}
	for _, r := range w.repos {
		if r.installationID == 0 {
			w.p.logger.Error("pooled repository not found in the App's installations, no warm runners for it", logkey.Repo, r.name)
		}
	}
}

// expire releases reservations older than reserveTimeout, for jobs GitHub ran elsewhere without telling us.
func (w *pool) expire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.p.now()
	for _, r := range w.repos {
		for id, at := range r.reserved {
			if now.Sub(at) > w.reserveTimeout {
				delete(r.reserved, id)
				w.p.logger.Warn("warm runner reservation expired", logkey.Repo, r.name, logkey.JobID, id)
			}
		}
	}
}

// retireIdle starts retiring every warm runner idle for longer than idleTimeout.
func (w *pool) retireIdle(ctx context.Context, wg *sync.WaitGroup) {
	if w.idleTimeout == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.p.now()
	for name, rn := range w.warm {
		r := rn.repo
		if !r.idle[name] || now.Sub(rn.idleSince) < w.idleTimeout {
			continue
		}
		delete(r.idle, name)
		r.retiring[name] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.retire(ctx, name, rn)
		}()
	}
}

// retire deletes the registration of the idle runner name, then stops its container. Deleting first means
// GitHub cannot hand it a job while it is being stopped. A failed delete puts it back to idle for the next sweep.
func (w *pool) retire(ctx context.Context, name string, rn *warmRunner) {
	log := w.p.logger.With(logkey.Repo, rn.repo.name, logkey.RunnerName, name, logkey.RunnerID, rn.runnerID)
	dctx, cancel := context.WithTimeout(ctx, jitTimeout)
	err := w.remover.DeleteRunner(dctx, rn.repo.installationID, rn.repo.name, rn.runnerID)
	cancel()
	var apiErr *github.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		// Already deregistered, so it cannot take a job either.
		err = nil
	}

	w.mu.Lock()
	retiring := rn.repo.retiring[name]
	if err != nil && retiring {
		delete(rn.repo.retiring, name)
		rn.repo.idle[name] = true
	}
	w.mu.Unlock()
	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.Warn("idle warm runner not retired, kept", logkey.Err, err)
		}
	case !retiring:
		log.Warn("warm runner took a job while its registration was deleted, left running")
	default:
		rn.stop()
		log.Info("idle warm runner retired", logkey.IdleTimeout, w.idleTimeout.String())
	}
}

// fill starts warm runners until every installed repository has size of them free, without exceeding max alive.
func (w *pool) fill(ctx context.Context, wg *sync.WaitGroup) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	for _, r := range w.repos {
		if r.installationID == 0 {
			continue
		}
		for r.available() < w.size && r.alive() < w.max {
			r.pending++
			wg.Add(1)
			go func() {
				defer wg.Done()
				w.runWarm(ctx, r)
			}()
		}
	}
}

// runWarm registers one warm runner for r and runs it until its job ends or ctx is cancelled.
func (w *pool) runWarm(ctx context.Context, r *warmRepo) {
	log := w.p.logger.With(logkey.Repo, r.name)
	name, cfg, err := w.p.register(ctx, r.installationID, r.name, w.newName, log)
	if err != nil {
		w.backOff(ctx, metrics.StageJITConfig, err, log)
		w.release(r)
		return
	}
	log = log.With(logkey.RunnerName, name)
	log.Info("warm runner registered", logkey.RunnerID, cfg.RunnerID)
	// Its own context lets retirement stop this runner alone.
	rctx, stop := context.WithCancel(ctx)
	defer stop()
	w.mu.Lock()
	r.pending--
	r.idle[name] = true
	w.warm[name] = &warmRunner{repo: r, runnerID: cfg.RunnerID, idleSince: w.p.now(), stop: stop}
	w.mu.Unlock()

	w.p.metrics.WorkersAlive.Inc()
	err = w.p.backend.Run(rctx, worker.Spec{
		RunnerName: name, JITConfig: cfg.Encoded, Repo: r.name, InstallationID: r.installationID, RunnerID: cfg.RunnerID,
	})
	w.p.metrics.WorkersAlive.Dec()

	w.mu.Lock()
	wasIdle, retired := r.idle[name], r.retiring[name]
	delete(r.idle, name)
	delete(r.retiring, name)
	delete(r.busy, name)
	delete(w.warm, name)
	if wasIdle {
		r.pending++
	}
	w.mu.Unlock()

	switch {
	case retired:
		w.signal()
		return
	case !wasIdle:
		// Its claim already refilled the pool, but its exit makes room under the cap.
		w.signal()
		if err != nil && ctx.Err() == nil {
			w.p.metrics.ProvisionFailures.WithLabelValues(metrics.StageWorker).Inc()
			log.Error("warm worker failed", logkey.Err, err)
			return
		}
		log.Info("warm runner done")
		return
	}
	if err != nil {
		w.backOff(ctx, metrics.StageWorker, err, log)
	} else {
		log.Warn("warm runner exited without a job")
	}
	w.release(r)
}

// release frees a pending slot whose runner is gone and wakes the pool to replace it.
func (w *pool) release(r *warmRepo) {
	w.mu.Lock()
	r.pending--
	w.mu.Unlock()
	w.signal()
}

// backOff logs a warm runner failure at stage and waits retryDelay, holding the caller's slot so a
// repository that keeps failing does not spin. Shutdown is not a failure and does not wait.
func (w *pool) backOff(ctx context.Context, stage string, err error, log *slog.Logger) {
	if ctx.Err() != nil {
		return
	}
	w.p.metrics.ProvisionFailures.WithLabelValues(stage).Inc()
	log.Error("warm runner failed", logkey.Stage, stage, logkey.Err, err)
	sleep(ctx, w.retryDelay)
}

// sleep waits for d or until ctx is cancelled, reporting whether the full wait elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
