package provisioner

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
	"github.com/TeddyBertrand/Mayfly/internal/retry"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

// fakeJIT records JIT requests and returns a fixed config or error.
type fakeJIT struct {
	mu     sync.Mutex
	names  []string
	labels []string
	err    error
	// failures, when set, limits err to the first failures calls.
	failures int
}

// GenerateJITConfig implements JITIssuer.
func (f *fakeJIT) GenerateJITConfig(_ context.Context, _ int64, _, name string, labels []string) (github.JITConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, name)
	f.labels = labels
	if f.err != nil && (f.failures == 0 || len(f.names) <= f.failures) {
		return github.JITConfig{}, f.err
	}
	return github.JITConfig{RunnerID: 1, Encoded: "SECRET-JIT"}, nil
}

// fakeBackend records specs and blocks each Run until release is closed or ctx ends.
type fakeBackend struct {
	mu      sync.Mutex
	specs   []worker.Spec
	started chan struct{}
	release chan struct{}
}

// newFakeBackend returns a fakeBackend whose Run returns immediately unless block is true.
func newFakeBackend(block bool) *fakeBackend {
	b := &fakeBackend{started: make(chan struct{}, 10), release: make(chan struct{})}
	if !block {
		close(b.release)
	}
	return b
}

// Run implements worker.Backend.
func (b *fakeBackend) Run(ctx context.Context, spec worker.Spec) error {
	b.mu.Lock()
	b.specs = append(b.specs, spec)
	b.mu.Unlock()
	b.started <- struct{}{}
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent writers and readers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// String returns the buffered text.
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// newTestProvisioner returns a Provisioner over the fakes and a buffer with its JSON logs.
func newTestProvisioner(jit *fakeJIT, b *fakeBackend, workers, queue int) (*Provisioner, *syncBuffer) {
	logs := &syncBuffer{}
	p := New(jit, b, []string{"mayfly"}, workers, queue, slog.New(slog.NewJSONHandler(logs, nil)))
	p.retry = retry.Policy{Attempts: 3, Base: time.Millisecond, Max: 5 * time.Millisecond}
	return p, logs
}

// waitStarted waits for n backend runs to start or fails the test.
func waitStarted(t *testing.T, b *fakeBackend, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-b.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d workers started", i, n)
		}
	}
}

var testJob = receiver.Job{ID: 4242, Repo: "octo/demo", InstallationID: 9}

// jobN returns testJob with a distinct ID, since the provisioner tracks one entry per job ID.
func jobN(i int) receiver.Job {
	j := testJob
	j.ID = int64(1000 + i)
	return j
}

// TestJobRunsOnWorker checks the runner name, labels and spec passed down for one job.
func TestJobRunsOnWorker(t *testing.T) {
	jit, b := &fakeJIT{}, newFakeBackend(false)
	p, logs := newTestProvisioner(jit, b, 1, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, b, 1)
	cancel()
	<-done

	if !strings.HasPrefix(jit.names[0], "mayfly-4242-") {
		t.Errorf("runner name = %q, want job id in it", jit.names[0])
	}
	if want := []string{"self-hosted", "linux", "x64", "mayfly"}; !reflect.DeepEqual(jit.labels, want) {
		t.Errorf("labels = %v, want %v", jit.labels, want)
	}
	spec := b.specs[0]
	if spec.JobID != 4242 || spec.RunnerName != jit.names[0] || spec.JITConfig != "SECRET-JIT" {
		t.Errorf("spec = %v", spec)
	}
	if strings.Contains(logs.String(), "SECRET-JIT") {
		t.Error("logs leak the JIT config")
	}
}

// TestJITFailureSkipsWorker checks a GitHub error is logged with the job ID and starts no container.
func TestJITFailureSkipsWorker(t *testing.T) {
	jit, b := &fakeJIT{err: errors.New("403 forbidden")}, newFakeBackend(false)
	p, logs := newTestProvisioner(jit, b, 1, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	_ = p.Enqueue(testJob)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "jit config failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if !strings.Contains(logs.String(), `"job_id":4242`) {
		t.Errorf("failure not logged with job_id:\n%s", logs.String())
	}
	if len(b.specs) != 0 {
		t.Errorf("worker started despite JIT failure")
	}
}

// TestEnqueueFullDoesNotBlock checks Enqueue returns ErrQueueFull at once when every slot is taken.
func TestEnqueueFullDoesNotBlock(t *testing.T) {
	p, _ := newTestProvisioner(&fakeJIT{}, newFakeBackend(true), 1, 2)
	for i := range 2 {
		if err := p.Enqueue(jobN(i)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	start := time.Now()
	if err := p.Enqueue(jobN(2)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("Enqueue blocked on a full queue")
	}
}

// TestShutdownStopsWorkersAndDropsQueue checks cancel stops running workers and logs dropped jobs.
func TestShutdownStopsWorkersAndDropsQueue(t *testing.T) {
	b := newFakeBackend(true)
	p, logs := newTestProvisioner(&fakeJIT{}, b, 2, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	for i := range 3 {
		_ = p.Enqueue(jobN(i))
	}
	waitStarted(t, b, 2)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if !strings.Contains(logs.String(), "queued job dropped at shutdown") {
		t.Errorf("third job not reported as dropped:\n%s", logs.String())
	}
	if n := strings.Count(logs.String(), "runner registered"); n != 2 {
		t.Errorf("runners registered = %d, want 2 (none after shutdown)", n)
	}
}

// TestRunnerLabels checks base labels come first and duplicates are removed case-insensitively.
func TestRunnerLabels(t *testing.T) {
	got := runnerLabels([]string{"Self-Hosted", "mayfly", "gpu"})
	want := []string{"self-hosted", "linux", "x64", "mayfly", "gpu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
