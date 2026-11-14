package reaper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

var now = time.Unix(1_700_000_000, 0)

// fakeWorkers is an in-memory Docker inventory; Remove deletes from it.
type fakeWorkers struct {
	mu        sync.Mutex
	workers   []worker.Info
	listErr   error
	removeErr error
}

// List implements worker.Inventory.
func (f *fakeWorkers) List(context.Context) ([]worker.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]worker.Info(nil), f.workers...), f.listErr
}

// Remove implements worker.Inventory.
func (f *fakeWorkers) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	for i, w := range f.workers {
		if w.ID == id {
			f.workers = append(f.workers[:i], f.workers[i+1:]...)
		}
	}
	return nil
}

// fakeRunners is a fake GitHub with one repo, octo/demo. Runner lookups come from runners (missing IDs
// return err); registrations and queued jobs come from regs and jobs, and deletes are recorded.
type fakeRunners struct {
	runners  map[int64]github.Runner
	err      error
	calls    int
	regs     []github.RunnerRegistration
	jobs     []github.QueuedJob
	reposErr error
	listErr  error
	deleted  []int64
}

// Runner implements GitHub.
func (f *fakeRunners) Runner(_ context.Context, _ int64, _ string, id int64) (github.Runner, error) {
	f.calls++
	if r, ok := f.runners[id]; ok {
		return r, nil
	}
	return github.Runner{}, f.err
}

// Repos implements GitHub.
func (f *fakeRunners) Repos(context.Context) ([]github.Repo, error) {
	return []github.Repo{{FullName: "octo/demo", InstallationID: 9}}, f.reposErr
}

// Runners implements GitHub.
func (f *fakeRunners) Runners(context.Context, int64, string) ([]github.RunnerRegistration, error) {
	return f.regs, f.listErr
}

// DeleteRunner implements GitHub and removes the registration from regs.
func (f *fakeRunners) DeleteRunner(_ context.Context, _ int64, _ string, id int64) error {
	f.deleted = append(f.deleted, id)
	for i, r := range f.regs {
		if r.ID == id {
			f.regs = append(f.regs[:i], f.regs[i+1:]...)
			break
		}
	}
	return nil
}

// QueuedJobs implements GitHub.
func (f *fakeRunners) QueuedJobs(context.Context, int64, string) ([]github.QueuedJob, error) {
	return f.jobs, f.listErr
}

var online = github.Runner{Found: true, Status: "online", Busy: true}

// workerInfo returns a labelled worker created age ago, owned by owner, with runner ID runnerID.
func workerInfo(id string, age time.Duration, owner string, runnerID int64) worker.Info {
	return worker.Info{ID: id, Created: now.Add(-age), JobID: 1, RunnerName: "mayfly-1-" + id, Owner: owner, Repo: "octo/demo", InstallationID: 9, RunnerID: runnerID}
}

// liveOwner returns an Owner that always answers id.
func liveOwner(id string) Owner {
	return func(context.Context) (string, error) { return id, nil }
}

// deadOwner is an Owner that never answers.
func deadOwner(context.Context) (string, error) { return "", errors.New("connection refused") }

var opts = Options{
	MaxAge: 6 * time.Hour, Grace: 2 * time.Minute, MissedProbes: 2,
	StaleAfter: 10 * time.Minute, StuckAfter: 10 * time.Minute, Labels: []string{"mayfly"},
}

// newTestReaper returns a Reaper over the fakes with a fixed clock and discarded logs.
func newTestReaper(w worker.Inventory, r GitHub, o Owner) *Reaper {
	rp := New(w, r, o, opts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rp.now = func() time.Time { return now }
	return rp
}

// reasons returns "id=reason" for each container removed in rep, sorted, for easy comparison.
func reasons(rep Report) []string {
	out := []string{}
	for _, r := range rep.Removed {
		out = append(out, r.Worker.ID+"="+r.Reason)
	}
	sort.Strings(out)
	return out
}

// TestSweepRules checks each orphan rule and the cases where the worker must be kept.
func TestSweepRules(t *testing.T) {
	tests := []struct {
		name    string
		worker  worker.Info
		runner  *github.Runner
		runErr  error
		owner   Owner
		want    []string
		wantGit bool
	}{
		{"healthy worker kept", workerInfo("a", 10*time.Minute, "live", 1), &online, nil, liveOwner("live"), []string{}, true},
		{"too old", workerInfo("a", 7*time.Hour, "live", 1), &online, nil, liveOwner("live"), []string{"a=" + ReasonMaxAge}, false},
		{"owner restarted", workerInfo("a", time.Minute, "old", 1), &online, nil, liveOwner("live"), []string{"a=" + ReasonOwnerGone}, false},
		{"no owner label", workerInfo("a", time.Minute, "", 1), &online, nil, liveOwner("live"), []string{"a=" + ReasonOwnerGone}, false},
		{"registration gone", workerInfo("a", 10*time.Minute, "live", 1), &github.Runner{Found: false}, nil, liveOwner("live"), []string{"a=" + ReasonRunnerGone}, true},
		{"runner offline", workerInfo("a", 10*time.Minute, "live", 1), &github.Runner{Found: true, Status: "offline"}, nil, liveOwner("live"), []string{"a=" + ReasonRunnerOffline}, true},
		{"offline but still booting", workerInfo("a", 30*time.Second, "live", 1), &github.Runner{Found: true, Status: "offline"}, nil, liveOwner("live"), []string{}, false},
		{"github error keeps worker", workerInfo("a", 10*time.Minute, "live", 1), nil, errors.New("502"), liveOwner("live"), []string{}, true},
		{"no runner id skips github", workerInfo("a", 10*time.Minute, "live", 0), nil, errors.New("unused"), liveOwner("live"), []string{}, false},
		{"owner blip keeps worker", workerInfo("a", 10*time.Minute, "live", 1), &online, nil, deadOwner, []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fw := &fakeWorkers{workers: []worker.Info{tt.worker}}
			fr := &fakeRunners{runners: map[int64]github.Runner{}, err: tt.runErr}
			if tt.runner != nil {
				fr.runners[tt.worker.RunnerID] = *tt.runner
			}
			got, err := newTestReaper(fw, fr, tt.owner).Sweep(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reasons(got), tt.want) {
				t.Errorf("removed = %v, want %v", reasons(got), tt.want)
			}
			if (fr.calls > 0) != tt.wantGit {
				t.Errorf("github calls = %d, want called = %v", fr.calls, tt.wantGit)
			}
		})
	}
}

// TestProvisionerKilledMidJob is the acceptance case: mayfly is killed mid-job and restarted with a new
// instance ID. Its running worker is busy and online on GitHub, yet one sweep leaves no worker behind.
func TestProvisionerKilledMidJob(t *testing.T) {
	fw := &fakeWorkers{workers: []worker.Info{workerInfo("a", 3*time.Minute, "before-crash", 1)}}
	fr := &fakeRunners{runners: map[int64]github.Runner{1: online}}

	got, err := newTestReaper(fw, fr, liveOwner("after-restart")).Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reasons(got), []string{"a=" + ReasonOwnerGone}) {
		t.Errorf("removed = %v", reasons(got))
	}
	if left, _ := fw.List(context.Background()); len(left) != 0 {
		t.Errorf("workers left after one sweep: %+v", left)
	}
}

// TestOwnerUnreachable checks workers survive one missed probe, go on the second, and the count resets on an answer.
func TestOwnerUnreachable(t *testing.T) {
	answer := false
	owner := func(ctx context.Context) (string, error) {
		if answer {
			return "live", nil
		}
		return deadOwner(ctx)
	}
	fw := &fakeWorkers{workers: []worker.Info{workerInfo("a", time.Minute, "live", 1)}}
	fr := &fakeRunners{runners: map[int64]github.Runner{1: online}}
	rp := newTestReaper(fw, fr, owner)
	ctx := context.Background()

	if got, _ := rp.Sweep(ctx); len(got.Removed) != 0 {
		t.Fatalf("first miss removed %v", reasons(got))
	}
	answer = true
	if got, _ := rp.Sweep(ctx); len(got.Removed) != 0 {
		t.Fatalf("answering owner removed %v", reasons(got))
	}
	answer = false
	if got, _ := rp.Sweep(ctx); len(got.Removed) != 0 {
		t.Fatalf("miss after reset removed %v", reasons(got))
	}
	if got, _ := rp.Sweep(ctx); !reflect.DeepEqual(reasons(got), []string{"a=" + ReasonOwnerUnreachable}) {
		t.Fatalf("second miss in a row removed %v", reasons(got))
	}
}

// TestSweepErrors checks a list failure is returned and a remove failure is skipped without stopping the sweep.
func TestSweepErrors(t *testing.T) {
	fr := &fakeRunners{}
	if _, err := newTestReaper(&fakeWorkers{listErr: errors.New("daemon down")}, fr, liveOwner("x")).Sweep(context.Background()); err == nil || !strings.Contains(err.Error(), "daemon down") {
		t.Errorf("err = %v, want list error", err)
	}

	fw := &fakeWorkers{workers: []worker.Info{workerInfo("a", 7*time.Hour, "x", 0), workerInfo("b", 7*time.Hour, "x", 0)}, removeErr: errors.New("busy")}
	got, err := newTestReaper(fw, fr, liveOwner("x")).Sweep(context.Background())
	if err != nil || len(got.Removed) != 0 {
		t.Errorf("got %v, %v; want no removals and no error", reasons(got), err)
	}
}

// TestHealthOwner checks the instance is read from /healthz and that bad answers are errors.
func TestHealthOwner(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"ok", http.StatusOK, `{"status":"ok","instance":"abc"}`, "abc", false},
		{"no instance", http.StatusOK, `{"status":"ok"}`, "", true},
		{"old plain text", http.StatusOK, "ok\n", "", true},
		{"server error", http.StatusServiceUnavailable, ``, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			got, err := HealthOwner(srv.Client(), srv.URL)(context.Background())
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("got %q, %v; want %q, err %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// TestRunStopsOnCancel checks Run sweeps at start and returns once ctx is cancelled.
func TestRunStopsOnCancel(t *testing.T) {
	fw := &fakeWorkers{workers: []worker.Info{workerInfo("a", 7*time.Hour, "x", 0)}}
	rp := newTestReaper(fw, &fakeRunners{}, liveOwner("x"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rp.Run(ctx, time.Hour); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if left, _ := fw.List(ctx); len(left) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if left, _ := fw.List(context.Background()); len(left) != 0 {
		t.Errorf("first sweep did not run: %+v", left)
	}
}
