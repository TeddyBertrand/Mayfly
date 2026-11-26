package provisioner

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

const noJobWarning = "warm runner exited without a job"

// manualClock is a clock the test moves while the pool's goroutines read it.
type manualClock struct{ nanos atomic.Int64 }

// now returns the clock's current time.
func (c *manualClock) now() time.Time { return time.Unix(0, c.nanos.Load()) }

// advance moves the clock forward by d.
func (c *manualClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

// waitExited waits until the pool remembers name as an exited runner.
func waitExited(t *testing.T, p *Provisioner, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		p.pool.mu.Lock()
		_, ok := p.pool.exited[name]
		p.pool.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("runner %s not remembered after its exit", name)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestLateJobEvents checks a warm job's claim time is recorded once, whatever the order of its events
// and whether they arrive before or after its runner's container is gone.
func TestLateJobEvents(t *testing.T) {
	const started, done = workflow.ActionInProgress, workflow.ActionCompleted
	tests := []struct {
		name       string
		exitFirst  bool
		events     []workflow.Action
		wantClaims uint64
	}{
		{name: "running, in order", events: []workflow.Action{started, done}, wantClaims: 1},
		{name: "running, reversed", events: []workflow.Action{done, started}, wantClaims: 1},
		{name: "exited, in order", exitFirst: true, events: []workflow.Action{started, done}, wantClaims: 1},
		{name: "exited, reversed", exitFirst: true, events: []workflow.Action{done, started}, wantClaims: 1},
		{name: "exited, redelivered", exitFirst: true, events: []workflow.Action{done, done, started, started}, wantClaims: 1},
		{name: "exited, in_progress lost", exitFirst: true, events: []workflow.Action{done}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &manualClock{}
			p, b, logs := startPool(t, 1, installed(), func(w *pool) { w.p.now = clock.now })
			first := b.next(t)
			if err := p.Enqueue(testJob); err != nil {
				t.Fatal(err)
			}
			b.next(t)
			clock.advance(2 * time.Second)
			if tt.exitFirst {
				b.finish(first)
				waitExited(t, p, first)
			}

			for _, action := range tt.events {
				p.Update(action, inProgress(testJob, first))
			}

			claims, sum := histogram(t, p.metrics.PoolClaimSeconds)
			if claims != tt.wantClaims || sum != float64(2*tt.wantClaims) {
				t.Errorf("claim = %d values, sum %v; want %d of 2s", claims, sum, tt.wantClaims)
			}
			// The reservation must stop counting at the first event, or the pool over-fills.
			if _, _, reserved := poolCounts(p); reserved != 0 {
				t.Errorf("reserved = %d, want 0", reserved)
			}
			if !tt.exitFirst {
				b.finish(first)
				waitExited(t, p, first)
			}
			clock.advance(reserveTimeout + time.Second)
			p.pool.expire()
			if strings.Contains(logs.String(), noJobWarning) {
				t.Errorf("warned about a runner that took a job:\n%s", logs.String())
			}
		})
	}
}

// TestExitWithoutEventsWarnsLate checks the warning waits out the window, then fires once and the record is dropped.
func TestExitWithoutEventsWarnsLate(t *testing.T) {
	clock := &manualClock{}
	p, b, logs := startPool(t, 1, installed(), func(w *pool) { w.p.now = clock.now })
	first := b.next(t)
	b.finish(first)
	b.next(t)
	waitExited(t, p, first)

	clock.advance(reserveTimeout)
	p.pool.expire()
	if strings.Contains(logs.String(), noJobWarning) {
		t.Fatal("warned before the window passed")
	}
	clock.advance(time.Second)
	p.pool.expire()
	p.pool.expire()
	if n := strings.Count(logs.String(), noJobWarning); n != 1 {
		t.Errorf("warnings = %d, want 1:\n%s", n, logs.String())
	}
	if warm, _ := p.pool.update(workflow.ActionInProgress, inProgress(testJob, first)); warm {
		t.Error("runner still recognised after the window")
	}
}

// TestAwaitedClaimExpires checks a claim whose in_progress never arrives is forgotten after the window.
func TestAwaitedClaimExpires(t *testing.T) {
	clock := &manualClock{}
	p, _, _ := idlePool("mayfly-warm-aaaaaa")
	p.now = clock.now
	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	p.Update(workflow.ActionCompleted, inProgress(testJob, "mayfly-warm-aaaaaa"))

	clock.advance(reserveTimeout + time.Second)
	p.pool.expire()
	if n := len(p.pool.awaiting); n != 0 {
		t.Errorf("awaited claims = %d, want 0", n)
	}
}

// TestWarmCompletedDropsColdWorker checks a completed event arriving first also drops the job's cold worker.
func TestWarmCompletedDropsColdWorker(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa")
	for _, i := range []int{1, 2} {
		if err := p.Enqueue(jobN(i)); err != nil {
			t.Fatal(err)
		}
	}
	p.Update(workflow.ActionCompleted, inProgress(jobN(2), "mayfly-warm-aaaaaa"))
	wantState(t, p, jobN(2).ID, Dropped)
}
