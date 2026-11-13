package provisioner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/receiver"
)

// TestTransitions checks every (from, to) pair against the documented state machine.
func TestTransitions(t *testing.T) {
	allowed := map[[2]State]bool{
		{Waiting, Provisioning}: true,
		{Waiting, Dropped}:      true,
		{Provisioning, Ready}:   true,
		{Provisioning, Dropped}: true,
		{Ready, Running}:        true,
		{Ready, Done}:           true,
		{Ready, Dropped}:        true,
		{Running, Done}:         true,
	}
	for from := Waiting; from <= Dropped; from++ {
		for to := Waiting; to <= Dropped; to++ {
			if got, want := canTransition(from, to), allowed[[2]State{from, to}]; got != want {
				t.Errorf("canTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestTarget checks the state each workflow_job action asks for.
func TestTarget(t *testing.T) {
	tests := []struct {
		action, conclusion string
		cur, want          State
	}{
		{"in_progress", "", Ready, Running},
		{"completed", "success", Running, Done},
		{"completed", "cancelled", Running, Done},
		{"completed", "cancelled", Waiting, Dropped},
		{"completed", "cancelled", Provisioning, Dropped},
		{"completed", "cancelled", Ready, Dropped},
		{"completed", "success", Ready, Done},
		{"completed", "failure", Waiting, Done},
	}
	for _, tt := range tests {
		if got := target(tt.action, tt.conclusion, tt.cur); got != tt.want {
			t.Errorf("target(%q, %q, %s) = %s, want %s", tt.action, tt.conclusion, tt.cur, got, tt.want)
		}
	}
}

// TestStateString checks names for known states and a fallback for unknown ones.
func TestStateString(t *testing.T) {
	if got := Running.String(); got != "running" {
		t.Errorf("Running.String() = %q", got)
	}
	if got := State(42).String(); got != "state(42)" {
		t.Errorf("State(42).String() = %q", got)
	}
}

// cancelled returns a completed event for id with conclusion "cancelled" and no runner.
func cancelled(id int64) receiver.Job {
	return receiver.Job{ID: id, Repo: "octo/demo", Conclusion: "cancelled"}
}

// startOne runs p with one blocking worker, enqueues jobN(0) and waits until its worker is up.
func startOne(t *testing.T) (*Provisioner, *fakeJIT, *fakeBackend, *syncBuffer) {
	t.Helper()
	jit, b := &fakeJIT{}, newFakeBackend(true)
	p, logs := newTestProvisioner(jit, b, 1, 10)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.Run(ctx)
	if err := p.Enqueue(jobN(0)); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, b, 1)
	return p, jit, b, logs
}

// wantState fails the test unless id is tracked in state want.
func wantState(t *testing.T, p *Provisioner, id int64, want State) {
	t.Helper()
	got, ok := p.jobs.state(id)
	if !ok || got != want {
		t.Fatalf("job %d state = %s (tracked %v), want %s", id, got, ok, want)
	}
}

// waitUntracked waits until id is no longer tracked, i.e. its worker is gone.
func waitUntracked(t *testing.T, p *Provisioner, id int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := p.jobs.state(id); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %d still tracked", id)
}

// TestCancelWhileWaiting checks a job cancelled in the queue never registers a runner.
func TestCancelWhileWaiting(t *testing.T) {
	p, jit, b, logs := startOne(t)
	queued := jobN(1)
	if err := p.Enqueue(queued); err != nil {
		t.Fatal(err)
	}
	p.Update("completed", cancelled(queued.ID))
	wantState(t, p, queued.ID, Dropped)

	close(b.release)
	waitUntracked(t, p, queued.ID)
	jit.mu.Lock()
	defer jit.mu.Unlock()
	if len(jit.names) != 1 {
		t.Errorf("runners registered = %d, want 1 (none for the cancelled job)", len(jit.names))
	}
	if !strings.Contains(logs.String(), "job dropped before start") {
		t.Errorf("skip not logged:\n%s", logs.String())
	}
}

// TestCancelWhileReady checks cancelling a job whose worker waits for pickup stops that worker.
func TestCancelWhileReady(t *testing.T) {
	p, _, _, logs := startOne(t)
	id := jobN(0).ID
	wantState(t, p, id, Ready)

	p.Update("completed", cancelled(id))
	waitUntracked(t, p, id)
	if !strings.Contains(logs.String(), "worker stopped, job cancelled before it started") {
		t.Errorf("stop not logged:\n%s", logs.String())
	}
}

// TestRunningJobIsNotKilled checks a runner that picked up a job, even another one, is matched by
// name, and that completing or cancelling that job never stops its worker.
func TestRunningJobIsNotKilled(t *testing.T) {
	p, jit, b, logs := startOne(t)
	id := jobN(0).ID
	jit.mu.Lock()
	runner := jit.names[0]
	jit.mu.Unlock()

	other := receiver.Job{ID: 777, Repo: "octo/demo", RunnerName: runner}
	p.Update("in_progress", other)
	wantState(t, p, id, Running)

	other.Conclusion = "cancelled"
	p.Update("completed", other)
	wantState(t, p, id, Done)

	close(b.release)
	waitUntracked(t, p, id)
	if !strings.Contains(logs.String(), `"msg":"job done"`) {
		t.Errorf("worker did not finish normally:\n%s", logs.String())
	}
}

// TestOutOfOrderEvents checks a late in_progress after completed is rejected, not applied.
func TestOutOfOrderEvents(t *testing.T) {
	p, _, _, logs := startOne(t)
	id := jobN(0).ID
	p.Update("completed", receiver.Job{ID: id, Conclusion: "success"})
	p.Update("in_progress", receiver.Job{ID: id})
	wantState(t, p, id, Done)
	if !strings.Contains(logs.String(), "transition done -> running not allowed") {
		t.Errorf("late in_progress not rejected:\n%s", logs.String())
	}
}

// TestDuplicateQueuedIgnored checks a second queued event for a tracked job adds no worker.
func TestDuplicateQueuedIgnored(t *testing.T) {
	p, _ := newTestProvisioner(&fakeJIT{}, newFakeBackend(true), 1, 10)
	for range 2 {
		if err := p.Enqueue(testJob); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(p.queue); n != 1 {
		t.Errorf("queued = %d, want 1", n)
	}
}

// TestQueueFullStopsTracking checks a job rejected by a full queue leaves no tracked entry.
func TestQueueFullStopsTracking(t *testing.T) {
	p, _ := newTestProvisioner(&fakeJIT{}, newFakeBackend(true), 1, 1)
	_ = p.Enqueue(jobN(0))
	if err := p.Enqueue(jobN(1)); err == nil {
		t.Fatal("second enqueue accepted, want ErrQueueFull")
	}
	if _, ok := p.jobs.state(jobN(1).ID); ok {
		t.Error("rejected job still tracked")
	}
}

// TestUpdateUntracked checks an event for a job this process never saw is logged and ignored.
func TestUpdateUntracked(t *testing.T) {
	p, logs := newTestProvisioner(&fakeJIT{}, newFakeBackend(true), 1, 1)
	p.Update("completed", cancelled(5))
	if !strings.Contains(logs.String(), "update for untracked job ignored") {
		t.Errorf("untracked update not logged:\n%s", logs.String())
	}
}
