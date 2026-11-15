package reaper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

// TestEveryReasonHasALabel checks no reason would be counted under an empty label.
func TestEveryReasonHasALabel(t *testing.T) {
	for _, r := range []string{ReasonMaxAge, ReasonOwnerGone, ReasonOwnerUnreachable, ReasonRunnerGone,
		ReasonRunnerOffline, ReasonMayflyUnreachable, ReasonNotPickedUp} {
		if reasonLabels[r] == "" {
			t.Errorf("no label for %q", r)
		}
	}
}

// TestReasonsPreset checks every reason series exists at 0 before the first sweep, each on the right metric.
func TestReasonsPreset(t *testing.T) {
	m := newTestReaper(&fakeWorkers{}, &fakeRunners{}, liveOwner("live")).metrics
	if got := testutil.CollectAndCount(m.WorkersRemoved); got != 5 {
		t.Errorf("worker reason series = %d, want 5", got)
	}
	if got := testutil.CollectAndCount(m.StuckJobs); got != 2 {
		t.Errorf("stuck reason series = %d, want 2", got)
	}
}

// TestSweepMetrics checks a sweep counts removed workers, deleted registrations and stuck jobs by reason,
// stamps its time, and that a failed container list counts as a failed sweep.
func TestSweepMetrics(t *testing.T) {
	fw := &fakeWorkers{workers: []worker.Info{workerInfo("old", 7*time.Hour, "live", 0)}}
	fr := &fakeRunners{
		regs: []github.RunnerRegistration{stale},
		jobs: []github.QueuedJob{{ID: 1, Labels: []string{"mayfly"}, CreatedAt: now.Add(-15 * time.Minute)}},
	}
	var clock time.Time
	rp := clockReaper(fw, fr, liveOwner("live"), &clock)
	m := rp.metrics

	sweepAt(t, rp, &clock, 0)
	sweepAt(t, rp, &clock, 10*time.Minute)

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"removed max_age", testutil.ToFloat64(m.WorkersRemoved.WithLabelValues("max_age")), 1},
		{"registrations", testutil.ToFloat64(m.RegistrationsRemoved), 1},
		{"stuck not_picked_up", testutil.ToFloat64(m.StuckJobs.WithLabelValues("not_picked_up")), 1},
		{"last sweep", testutil.ToFloat64(m.LastSweep), float64(now.Add(10 * time.Minute).Unix())},
		{"sweep failures", testutil.ToFloat64(m.SweepFailures), 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	fw.listErr = errors.New("daemon down")
	if _, err := rp.Sweep(context.Background()); err == nil {
		t.Fatal("sweep with a failed list returned no error")
	}
	if got := testutil.ToFloat64(m.SweepFailures); got != 1 {
		t.Errorf("sweep failures = %v, want 1", got)
	}
}
