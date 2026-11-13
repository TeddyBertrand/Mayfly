package reaper

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// stale is an offline mayfly registration with no container behind it.
var stale = github.RunnerRegistration{ID: 25, Name: "mayfly-132071681699-cc99e4", Status: "offline", Busy: true}

// clockReaper returns a test reaper whose clock reads *clock, so a test can move time forward.
func clockReaper(fw *fakeWorkers, fr *fakeRunners, o Owner, clock *time.Time) *Reaper {
	rp := newTestReaper(fw, fr, o)
	rp.now = func() time.Time { return *clock }
	return rp
}

// sweepAt sets the clock to start+offset and runs one sweep.
func sweepAt(t *testing.T, rp *Reaper, clock *time.Time, offset time.Duration) Report {
	t.Helper()
	*clock = now.Add(offset)
	rep, err := rp.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestStaleRegistrationWindow checks a stale registration is kept until it has been stale for StaleAfter, then deleted once.
func TestStaleRegistrationWindow(t *testing.T) {
	fr := &fakeRunners{regs: []github.RunnerRegistration{stale}}
	var clock time.Time
	rp := clockReaper(&fakeWorkers{}, fr, liveOwner("live"), &clock)

	for _, at := range []time.Duration{0, 5 * time.Minute, 9 * time.Minute} {
		if rep := sweepAt(t, rp, &clock, at); len(rep.Deregistered) != 0 {
			t.Fatalf("deleted at +%s, before the window ended", at)
		}
	}
	rep := sweepAt(t, rp, &clock, 10*time.Minute)
	if !reflect.DeepEqual(rep.Deregistered, []github.RunnerRegistration{stale}) || !reflect.DeepEqual(fr.deleted, []int64{25}) {
		t.Fatalf("at +10m deregistered %+v, deleted %v", rep.Deregistered, fr.deleted)
	}
	if rep := sweepAt(t, rp, &clock, 11*time.Minute); len(rep.Deregistered) != 0 || len(fr.deleted) != 1 {
		t.Errorf("deleted twice: %v", fr.deleted)
	}
}

// TestRegistrationsNeverDeleted checks the registrations that must survive any number of sweeps.
func TestRegistrationsNeverDeleted(t *testing.T) {
	tests := []struct {
		name    string
		reg     github.RunnerRegistration
		workers []docker.Worker
	}{
		{"online", github.RunnerRegistration{ID: 1, Name: "mayfly-1-aa", Status: "online"}, nil},
		{"not a mayfly runner", github.RunnerRegistration{ID: 2, Name: "build-box", Status: "offline"}, nil},
		{"container still booting", github.RunnerRegistration{ID: 3, Name: "mayfly-3-aa", Status: "offline"},
			[]docker.Worker{worker("c", 30*time.Second, "live", 3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &fakeRunners{regs: []github.RunnerRegistration{tt.reg}, runners: map[int64]github.Runner{tt.reg.ID: {Found: true, Status: tt.reg.Status}}}
			var clock time.Time
			rp := clockReaper(&fakeWorkers{workers: tt.workers}, fr, liveOwner("live"), &clock)
			for _, at := range []time.Duration{0, time.Hour, 2 * time.Hour} {
				// Keep the booting container young, so only the registration rule is under test.
				for i := range tt.workers {
					tt.workers[i].Created = now.Add(at - 30*time.Second)
				}
				sweepAt(t, rp, &clock, at)
			}
			if len(fr.deleted) != 0 {
				t.Errorf("deleted %v", fr.deleted)
			}
		})
	}
}

// TestRemovedContainerFreesRegistration checks a container removed by this sweep stops claiming its registration.
func TestRemovedContainerFreesRegistration(t *testing.T) {
	reg := github.RunnerRegistration{ID: 7, Name: "mayfly-7-aa", Status: "offline"}
	fw := &fakeWorkers{workers: []docker.Worker{worker("old", time.Minute, "before-crash", 7)}}
	fr := &fakeRunners{regs: []github.RunnerRegistration{reg}}
	var clock time.Time
	rp := clockReaper(fw, fr, liveOwner("after-restart"), &clock)

	if rep := sweepAt(t, rp, &clock, 0); len(rep.Removed) != 1 || len(rep.Deregistered) != 0 {
		t.Fatalf("first sweep: %+v", rep)
	}
	if rep := sweepAt(t, rp, &clock, 10*time.Minute); !reflect.DeepEqual(fr.deleted, []int64{7}) {
		t.Errorf("registration not deleted after the window: %+v", rep)
	}
}

// TestStaleWindowRestarts checks the window starts over after the runner comes back, and after a reaper restart.
func TestStaleWindowRestarts(t *testing.T) {
	fr := &fakeRunners{regs: []github.RunnerRegistration{stale}}
	var clock time.Time
	rp := clockReaper(&fakeWorkers{}, fr, liveOwner("live"), &clock)

	sweepAt(t, rp, &clock, 0)
	fr.regs = []github.RunnerRegistration{{ID: 25, Name: stale.Name, Status: "online"}}
	sweepAt(t, rp, &clock, 5*time.Minute)
	fr.regs = []github.RunnerRegistration{stale}
	sweepAt(t, rp, &clock, 6*time.Minute)
	if sweepAt(t, rp, &clock, 12*time.Minute); len(fr.deleted) != 0 {
		t.Fatalf("deleted 6m after going stale again")
	}

	fresh := clockReaper(&fakeWorkers{}, fr, liveOwner("live"), &clock)
	if sweepAt(t, fresh, &clock, 20*time.Minute); len(fr.deleted) != 0 {
		t.Errorf("a restarted reaper deleted without its own window")
	}
}

// TestGitHubFailuresAreSafe checks GitHub errors never delete anything and never block container cleanup.
func TestGitHubFailuresAreSafe(t *testing.T) {
	fw := &fakeWorkers{workers: []docker.Worker{worker("old", 7*time.Hour, "live", 0)}}
	fr := &fakeRunners{regs: []github.RunnerRegistration{stale}, reposErr: errors.New("502")}
	var clock time.Time
	rp := clockReaper(fw, fr, liveOwner("live"), &clock)
	if rep := sweepAt(t, rp, &clock, 0); len(rep.Removed) != 1 {
		t.Fatalf("container pass blocked by a GitHub error: %+v", rep)
	}

	fr.reposErr = nil
	sweepAt(t, rp, &clock, time.Minute)
	fr.listErr = errors.New("502")
	sweepAt(t, rp, &clock, 5*time.Minute)
	if len(fr.deleted) != 0 {
		t.Fatalf("deleted on a failed list")
	}
	fr.listErr = nil
	if sweepAt(t, rp, &clock, 11*time.Minute); !reflect.DeepEqual(fr.deleted, []int64{25}) {
		t.Errorf("a failed list reset the window: deleted %v", fr.deleted)
	}
}

// TestStuckJobs checks old mayfly jobs are reported once with the right reason, and others are ignored.
func TestStuckJobs(t *testing.T) {
	job := func(id int64, age time.Duration, labels ...string) github.QueuedJob {
		return github.QueuedJob{ID: id, Name: "build", Labels: labels, CreatedAt: now.Add(-age), URL: "https://gh/j"}
	}
	fr := &fakeRunners{jobs: []github.QueuedJob{
		job(1, 15*time.Minute, "self-hosted", "Mayfly"),
		job(2, 15*time.Minute, "ubuntu-latest"),
		job(3, 2*time.Minute, "mayfly"),
	}}
	var clock time.Time
	rp := clockReaper(&fakeWorkers{}, fr, liveOwner("live"), &clock)

	rep := sweepAt(t, rp, &clock, 0)
	if len(rep.Stuck) != 1 || rep.Stuck[0].Job.ID != 1 || rep.Stuck[0].Reason != ReasonNotPickedUp || rep.Stuck[0].Repo != "octo/demo" {
		t.Fatalf("stuck = %+v", rep.Stuck)
	}
	if rep := sweepAt(t, rp, &clock, time.Minute); len(rep.Stuck) != 0 {
		t.Errorf("job 1 reported twice: %+v", rep.Stuck)
	}

	down := clockReaper(&fakeWorkers{}, fr, deadOwner, &clock)
	if rep := sweepAt(t, down, &clock, 0); len(rep.Stuck) != 1 || rep.Stuck[0].Reason != ReasonMayflyUnreachable {
		t.Errorf("with mayfly down, stuck = %+v", rep.Stuck)
	}
}
