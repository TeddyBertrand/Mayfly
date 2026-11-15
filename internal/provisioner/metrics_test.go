package provisioner

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// histogram returns how many values h holds and their sum.
func histogram(t *testing.T, h prometheus.Histogram) (uint64, float64) {
	t.Helper()
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

// TestJobMetricsFromGitHubTimestamps checks queue time, duration and conclusions come from the event, even for untracked jobs,
// and that a job no runner took has no duration.
func TestJobMetricsFromGitHubTimestamps(t *testing.T) {
	p, _ := newTestProvisioner(&fakeJIT{}, newFakeBackend(false), Options{Workers: 1, QueueSize: 1})
	m := p.metrics
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	p.Update(workflow.ActionInProgress, workflow.Job{ID: 1, RunnerName: "mayfly-1-aa", CreatedAt: t0, StartedAt: t0.Add(12 * time.Second)})
	p.Update(workflow.ActionCompleted, workflow.Job{ID: 1, RunnerName: "mayfly-1-aa", Conclusion: "success",
		CreatedAt: t0, StartedAt: t0.Add(12 * time.Second), CompletedAt: t0.Add(72 * time.Second)})
	p.Update(workflow.ActionCompleted, workflow.Job{ID: 2, Conclusion: "cancelled", CreatedAt: t0, StartedAt: t0, CompletedAt: t0.Add(time.Minute)})
	p.Update(workflow.ActionInProgress, workflow.Job{ID: 3, CreatedAt: t0})

	if n, sum := histogram(t, m.QueueSeconds); n != 1 || sum != 12 {
		t.Errorf("queue = %d values, sum %v; want 1, 12", n, sum)
	}
	if n, sum := histogram(t, m.JobSeconds); n != 1 || sum != 60 {
		t.Errorf("duration = %d values, sum %v; want 1, 60", n, sum)
	}
	for conclusion, want := range map[string]float64{"success": 1, "cancelled": 1} {
		if got := testutil.ToFloat64(m.Jobs.WithLabelValues(conclusion)); got != want {
			t.Errorf("jobs{%s} = %v, want %v", conclusion, got, want)
		}
	}
	if n, _ := histogram(t, m.BootSeconds); n != 0 {
		t.Errorf("boot time recorded for untracked jobs: %d", n)
	}
}

// TestBootTimeAndGauges checks boot time runs from Enqueue to in_progress on mayfly's clock,
// and the waiting and alive gauges follow the job.
func TestBootTimeAndGauges(t *testing.T) {
	b := newFakeBackend(true)
	p, _ := newTestProvisioner(&fakeJIT{}, b, Options{Workers: 1, QueueSize: 2})
	m := p.metrics
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return t0 }

	if err := p.Enqueue(testJob); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(m.JobsWaiting); got != 1 {
		t.Errorf("waiting before run = %v, want 1", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	waitStarted(t, b, 1)
	if waiting, alive := testutil.ToFloat64(m.JobsWaiting), testutil.ToFloat64(m.WorkersAlive); waiting != 0 || alive != 1 {
		t.Errorf("while running: waiting = %v, alive = %v; want 0, 1", waiting, alive)
	}

	p.now = func() time.Time { return t0.Add(8 * time.Second) }
	p.Update(workflow.ActionInProgress, workflow.Job{ID: testJob.ID, RunnerName: b.specs[0].RunnerName})
	if n, sum := histogram(t, m.BootSeconds); n != 1 || sum != 8 {
		t.Errorf("boot = %d values, sum %v; want 1, 8", n, sum)
	}

	close(b.release)
	deadline := time.Now().Add(2 * time.Second)
	for testutil.ToFloat64(m.WorkersAlive) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if got := testutil.ToFloat64(m.WorkersAlive); got != 0 {
		t.Errorf("alive after the worker exited = %v, want 0", got)
	}
}

// TestJITFailureMetrics checks each retry and the final give-up are counted.
func TestJITFailureMetrics(t *testing.T) {
	p, logs := newTestProvisioner(&fakeJIT{err: flakyErr{}}, newFakeBackend(false), Options{Workers: 1, QueueSize: 10})

	runUntilLogged(t, p, logs, `"msg":"jit config failed"`)

	if got := testutil.ToFloat64(p.metrics.Retries.WithLabelValues(metrics.OpJITConfig)); got != 2 {
		t.Errorf("retries = %v, want 2", got)
	}
	if got := testutil.ToFloat64(p.metrics.ProvisionFailures.WithLabelValues(metrics.StageJITConfig)); got != 1 {
		t.Errorf("failures = %v, want 1", got)
	}
}
