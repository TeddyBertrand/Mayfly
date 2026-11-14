package provisioner

import (
	"context"
	"strings"
	"testing"
	"time"
)

// flakyErr is an injected GitHub failure that asks to be retried.
type flakyErr struct{}

// Error implements error.
func (flakyErr) Error() string { return "github POST /runners: 502 bad gateway" }

// Temporary marks the error as worth another attempt.
func (flakyErr) Temporary() bool { return true }

// runUntilLogged runs p, enqueues testJob, and stops once logs contain want or two seconds pass.
func runUntilLogged(t *testing.T, p *Provisioner, logs *syncBuffer, want string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	_ = p.Enqueue(testJob)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), want) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("logs never contained %q:\n%s", want, logs.String())
	}
}

// TestJITRetriedThenSucceeds checks temporary failures are retried with a fresh runner name each time.
func TestJITRetriedThenSucceeds(t *testing.T) {
	jit, b := &fakeJIT{err: flakyErr{}, failures: 2}, newFakeBackend(false)
	p, logs := newTestProvisioner(jit, b, Options{Workers: 1, QueueSize: 10})

	runUntilLogged(t, p, logs, "job done")

	if len(jit.names) != 3 {
		t.Fatalf("jit calls = %d, want 3", len(jit.names))
	}
	seen := map[string]bool{}
	for _, n := range jit.names {
		if seen[n] {
			t.Errorf("runner name %q reused across attempts", n)
		}
		seen[n] = true
	}
	if len(b.specs) != 1 || b.specs[0].RunnerName != jit.names[2] {
		t.Errorf("worker should run with the last registered name %q, got %+v", jit.names[2], b.specs)
	}
	if got := strings.Count(logs.String(), "jit config failed, retrying"); got != 2 {
		t.Errorf("retry warnings = %d, want 2", got)
	}
}

// TestJITGivesUp checks a failure that never clears stops after the policy's attempts with a clear error.
func TestJITGivesUp(t *testing.T) {
	jit, b := &fakeJIT{err: flakyErr{}}, newFakeBackend(false)
	p, logs := newTestProvisioner(jit, b, Options{Workers: 1, QueueSize: 10})

	runUntilLogged(t, p, logs, `"msg":"jit config failed"`)

	if len(jit.names) != 3 {
		t.Errorf("jit calls = %d, want 3", len(jit.names))
	}
	if !strings.Contains(logs.String(), "gave up after 3 attempts: github POST /runners: 502 bad gateway") {
		t.Errorf("final error not clear:\n%s", logs.String())
	}
	if len(b.specs) != 0 {
		t.Errorf("worker started despite JIT failure")
	}
}
