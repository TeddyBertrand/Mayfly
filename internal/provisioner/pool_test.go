package provisioner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// fakeLister returns fixed repositories, or err for the first failures calls.
type fakeLister struct {
	mu       sync.Mutex
	repos    []github.Repo
	err      error
	failures int
	calls    int
}

// Repos implements RepoLister.
func (f *fakeLister) Repos(context.Context) ([]github.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil && f.calls <= f.failures {
		return nil, f.err
	}
	return f.repos, nil
}

// runnerBackend runs every worker until the test finishes that runner by name, or ctx ends.
type runnerBackend struct {
	mu      sync.Mutex
	specs   []worker.Spec
	done    map[string]chan struct{}
	started chan string
}

// newRunnerBackend returns an empty runnerBackend.
func newRunnerBackend() *runnerBackend {
	return &runnerBackend{done: map[string]chan struct{}{}, started: make(chan string, 20)}
}

// Run implements worker.Backend.
func (b *runnerBackend) Run(ctx context.Context, spec worker.Spec) error {
	b.mu.Lock()
	b.specs = append(b.specs, spec)
	done := make(chan struct{})
	b.done[spec.RunnerName] = done
	b.mu.Unlock()
	b.started <- spec.RunnerName
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// finish makes the runner called name exit, as when its job ends.
func (b *runnerBackend) finish(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.done[name])
}

// startedSpecs returns a copy of the specs of every worker started so far.
func (b *runnerBackend) startedSpecs() []worker.Spec {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]worker.Spec(nil), b.specs...)
}

// next waits for the next worker to start and returns its runner name.
func (b *runnerBackend) next(t *testing.T) string {
	t.Helper()
	select {
	case name := <-b.started:
		return name
	case <-time.After(2 * time.Second):
		t.Fatal("no worker started")
		return ""
	}
}

// none fails the test if a worker starts within a short wait.
func (b *runnerBackend) none(t *testing.T) {
	t.Helper()
	select {
	case name := <-b.started:
		t.Fatalf("unexpected worker %s started", name)
	case <-time.After(50 * time.Millisecond):
	}
}

// pooledProvisioner returns a provisioner pooling testJob's repository with size warm runners, not yet running.
func pooledProvisioner(size int, lister *fakeLister) (*Provisioner, *runnerBackend, *syncBuffer) {
	b := newRunnerBackend()
	p, logs := newTestProvisioner(&fakeJIT{}, b, Options{Workers: 1, QueueSize: 10, Pool: PoolOptions{
		Repos: []string{testJob.Repo}, Size: size, Installations: lister,
	}})
	p.pool.retryDelay = time.Millisecond
	return p, b, logs
}

// startPool runs a pooled provisioner with size warm runners and stops it when the test ends.
func startPool(t *testing.T, size int, lister *fakeLister) (*Provisioner, *runnerBackend, *syncBuffer) {
	t.Helper()
	p, b, logs := pooledProvisioner(size, lister)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return p, b, logs
}

// idlePool returns a pooled provisioner whose loop never runs, with testJob's installation known
// and the given runners idle, so a test sets the pool state by hand.
func idlePool(runners ...string) (*Provisioner, *runnerBackend, *syncBuffer) {
	p, b, logs := pooledProvisioner(1, installed())
	r := p.pool.repos[repoKey(testJob.Repo)]
	r.installationID = testJob.InstallationID
	for _, name := range runners {
		r.idle[name] = true
		p.pool.warm[name] = r
	}
	return p, b, logs
}

// installed lists testJob's repository under testJob's installation.
func installed() *fakeLister {
	return &fakeLister{repos: []github.Repo{{FullName: testJob.Repo, InstallationID: testJob.InstallationID}}}
}

// inProgress returns job as reported running on runner.
func inProgress(job workflow.Job, runner string) workflow.Job {
	job.RunnerName = runner
	return job
}

// poolCounts returns the idle, pending and reserved counts of testJob's repository.
func poolCounts(p *Provisioner) (idle, pending, reserved int) {
	p.pool.mu.Lock()
	defer p.pool.mu.Unlock()
	r := p.pool.repos[repoKey(testJob.Repo)]
	return len(r.idle), r.pending, len(r.reserved)
}

// wantCounts fails the test unless testJob's repository has the given counts.
func wantCounts(t *testing.T, p *Provisioner, idle, pending, reserved int) {
	t.Helper()
	if i, pe, r := poolCounts(p); i != idle || pe != pending || r != reserved {
		t.Fatalf("pool idle/pending/reserved = %d/%d/%d, want %d/%d/%d", i, pe, r, idle, pending, reserved)
	}
}

// waitCounts waits until testJob's repository has the given counts or fails the test.
func waitCounts(t *testing.T, p *Provisioner, idle, pending, reserved int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		i, pe, r := poolCounts(p)
		if i == idle && pe == pending && r == reserved {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pool idle/pending/reserved = %d/%d/%d, want %d/%d/%d", i, pe, r, idle, pending, reserved)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPoolFillsAtStart checks the pool registers size runners for the repository's installation, with no job.
func TestPoolFillsAtStart(t *testing.T) {
	p, b, _ := startPool(t, 2, installed())
	b.next(t)
	b.next(t)
	b.none(t)
	waitCounts(t, p, 2, 0, 0)

	for _, s := range b.startedSpecs() {
		if !strings.HasPrefix(s.RunnerName, "mayfly-warm-") || s.JobID != 0 {
			t.Errorf("warm spec = %+v, want a mayfly-warm- name and no job", s)
		}
		if s.Repo != testJob.Repo || s.InstallationID != testJob.InstallationID || s.JITConfig != "SECRET-JIT" {
			t.Errorf("warm spec = %+v, want the repository's installation and its JIT config", s)
		}
	}
}

// TestQueuedJobReservesAndRefills checks a queued job takes no cold path and the pool starts a replacement.
func TestQueuedJobReservesAndRefills(t *testing.T) {
	p, b, _ := startPool(t, 1, installed())
	first := b.next(t)

	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	if _, tracked := p.jobs.state(testJob.ID); tracked {
		t.Error("reserved job was also queued for a cold worker")
	}
	b.next(t)
	waitCounts(t, p, 2, 0, 1)

	p.Update(workflow.ActionInProgress, inProgress(testJob, first))
	waitCounts(t, p, 1, 0, 0)
	b.none(t)
	if n, _ := histogram(t, p.metrics.BootSeconds); n != 1 {
		t.Errorf("boot time observations = %d, want 1 for the warm job", n)
	}
}

// TestClaimedRunnerNotReused checks a warm runner that ran a job is gone when it exits and is not replaced again.
func TestClaimedRunnerNotReused(t *testing.T) {
	p, b, _ := startPool(t, 1, installed())
	first := b.next(t)
	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	second := b.next(t)
	p.Update(workflow.ActionInProgress, inProgress(testJob, first))

	b.finish(first)
	b.none(t)
	waitCounts(t, p, 1, 0, 0)
	p.pool.mu.Lock()
	_, firstWarm := p.pool.warm[first]
	_, secondWarm := p.pool.warm[second]
	p.pool.mu.Unlock()
	if firstWarm || !secondWarm {
		t.Errorf("warm after the job: first = %v, second = %v; want false, true", firstWarm, secondWarm)
	}
}

// TestUnexpectedClaimRefills checks a warm runner taking a job the pool never reserved is replaced.
func TestUnexpectedClaimRefills(t *testing.T) {
	p, b, _ := startPool(t, 1, installed())
	first := b.next(t)

	p.Update(workflow.ActionInProgress, inProgress(testJob, first))
	b.next(t)
	waitCounts(t, p, 1, 0, 0)
}

// TestIdleExitRefills checks a warm runner that exits without a job is replaced.
func TestIdleExitRefills(t *testing.T) {
	p, b, _ := startPool(t, 1, installed())
	first := b.next(t)

	b.finish(first)
	b.next(t)
	waitCounts(t, p, 1, 0, 0)
}

// TestUninstalledRepoNotPooled checks a repository missing from the installations gets no runners and goes cold.
func TestUninstalledRepoNotPooled(t *testing.T) {
	p, b, logs := startPool(t, 1, &fakeLister{})
	b.none(t)
	if !strings.Contains(logs.String(), "pooled repository not found") {
		t.Errorf("missing installation not logged: %s", logs.String())
	}

	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.next(t), "mayfly-4242-") {
		t.Error("job did not get a cold worker")
	}
}

// TestInstallationLookupRetried checks the pool keeps looking up installations until GitHub answers.
func TestInstallationLookupRetried(t *testing.T) {
	lister := installed()
	lister.err, lister.failures = errors.New("github GET /app/installations: 502"), 2
	_, b, _ := startPool(t, 1, lister)
	b.next(t)
}

// TestPoolMissGoesCold checks a job finding no free warm runner is tracked for a cold worker.
func TestPoolMissGoesCold(t *testing.T) {
	p, _, logs := idlePool("mayfly-warm-aaaaaa")
	if err := p.Enqueue(jobN(1)); err != nil {
		t.Fatal(err)
	}
	if err := p.Enqueue(jobN(2)); err != nil {
		t.Fatal(err)
	}
	if _, tracked := p.jobs.state(jobN(1).ID); tracked {
		t.Error("first job was queued cold despite a free warm runner")
	}
	wantState(t, p, jobN(2).ID, Waiting)
	if !strings.Contains(logs.String(), "no warm runner free, cold start") {
		t.Errorf("pool miss not logged: %s", logs.String())
	}
}

// TestDuplicateQueuedReservesOnce checks a redelivered queued event does not take a second runner.
func TestDuplicateQueuedReservesOnce(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa", "mayfly-warm-bbbbbb")
	for range 2 {
		if err := p.Enqueue(testJob); err != nil {
			t.Fatal(err)
		}
	}
	wantCounts(t, p, 2, 0, 1)
}

// TestWarmClaimDropsColdWorker checks a cold worker started for a job a warm runner took is dropped.
func TestWarmClaimDropsColdWorker(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa")
	if err := p.Enqueue(jobN(1)); err != nil {
		t.Fatal(err)
	}
	if err := p.Enqueue(jobN(2)); err != nil {
		t.Fatal(err)
	}
	wantState(t, p, jobN(2).ID, Waiting)

	p.Update(workflow.ActionInProgress, inProgress(jobN(2), "mayfly-warm-aaaaaa"))
	wantState(t, p, jobN(2).ID, Dropped)
	// Job 1 still counts on a runner, and the one taken must be replaced for it.
	wantCounts(t, p, 0, 0, 1)
	select {
	case <-p.pool.wake:
	default:
		t.Error("taking a runner for an unreserved job did not wake the pool")
	}
}

// TestReservationReleased checks a reservation ends when its job is cancelled or runs on another runner.
func TestReservationReleased(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa")
	if err := p.Enqueue(jobN(1)); err != nil {
		t.Fatal(err)
	}
	p.Update(workflow.ActionCompleted, cancelled(jobN(1).ID))
	wantCounts(t, p, 1, 0, 0)

	if err := p.Enqueue(jobN(2)); err != nil {
		t.Fatal(err)
	}
	p.Update(workflow.ActionInProgress, inProgress(jobN(2), "mayfly-77-abcdef"))
	wantCounts(t, p, 1, 0, 0)
}

// TestReservationExpires checks a reservation nothing ever answered stops counting after reserveTimeout.
func TestReservationExpires(t *testing.T) {
	p, _, logs := idlePool("mayfly-warm-aaaaaa")
	now := time.Now()
	p.now = func() time.Time { return now }
	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}

	now = now.Add(reserveTimeout)
	p.pool.expire()
	wantCounts(t, p, 1, 0, 1)

	now = now.Add(time.Second)
	p.pool.expire()
	wantCounts(t, p, 1, 0, 0)
	if !strings.Contains(logs.String(), "reservation expired") {
		t.Errorf("expiry not logged: %s", logs.String())
	}
}

// TestNoPoolWithoutRepos checks the zero PoolOptions leaves the provisioner as before.
func TestNoPoolWithoutRepos(t *testing.T) {
	p, _ := newTestProvisioner(&fakeJIT{}, newFakeBackend(false), Options{Workers: 1, QueueSize: 1})
	if p.pool != nil {
		t.Fatal("pool created with no repositories")
	}
}
