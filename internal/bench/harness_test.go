package bench

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
)

// fakeAPI hands out run IDs in order, reports each run in progress for polls polls, then completed with conclusion.
type fakeAPI struct {
	polls      int
	conclusion func(runner string) string
	// stuck reports whether run id stays in progress forever; nil means no run is stuck.
	stuck     func(id int64) bool
	cancelled []int64
	pollErrs  int
	// jobLag is how many jobs calls per run still report in_progress, as GitHub's jobs endpoint can after the run completed.
	jobLag  int
	jobSeen map[int64]int

	nextID     int64
	runner     map[int64]string
	seen       map[int64]int
	dispatched []map[string]string
}

// DispatchWorkflow records the inputs and returns the next run ID.
func (f *fakeAPI) DispatchWorkflow(_ context.Context, _, file, _ string, inputs map[string]string) (int64, error) {
	f.nextID++
	if f.runner == nil {
		f.runner, f.seen = map[int64]string{}, map[int64]int{}
	}
	f.runner[f.nextID] = inputs["runner"]
	f.dispatched = append(f.dispatched, map[string]string{"file": file, "runner": inputs["runner"]})
	return f.nextID, nil
}

// Run fails the first pollErrs polls, then reports in_progress until polls are used up.
func (f *fakeAPI) Run(_ context.Context, _ string, id int64) (github.WorkflowRun, error) {
	if f.pollErrs > 0 {
		f.pollErrs--
		return github.WorkflowRun{}, errors.New("connection reset")
	}
	f.seen[id]++
	if (f.stuck != nil && f.stuck(id)) || f.seen[id] <= f.polls {
		return github.WorkflowRun{ID: id, Status: "in_progress"}, nil
	}
	return github.WorkflowRun{ID: id, Status: github.RunStatusCompleted, Conclusion: f.conclusion(f.runner[id])}, nil
}

// RunJobs returns one job taking 8 s to start, 12 s to the first step and 40 s in total, in progress for the first jobLag calls.
func (f *fakeAPI) RunJobs(_ context.Context, _ string, id int64) ([]github.Job, error) {
	if f.jobSeen == nil {
		f.jobSeen = map[int64]int{}
	}
	f.jobSeen[id]++
	status := github.RunStatusCompleted
	if f.jobSeen[id] <= f.jobLag {
		status = "in_progress"
	}
	return []github.Job{{
		ID: id * 10, RunnerName: f.runner[id] + "-1", Status: status,
		CreatedAt: at(0), StartedAt: at(8), CompletedAt: at(40),
		Steps: []github.Step{{Number: 1, Name: "Set up job", StartedAt: at(10)}, {Number: 2, Name: "Run x", StartedAt: at(12)}},
	}}, nil
}

// CancelRun records the cancel and refuses it like GitHub does for a run stuck before its job exists.
func (f *fakeAPI) CancelRun(_ context.Context, _ string, id int64) error {
	f.cancelled = append(f.cancelled, id)
	return errors.New("409 Conflict")
}

// testHarness returns a harness over api that polls every millisecond.
func testHarness(api API) (*Harness, Config) {
	h := &Harness{API: api, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return at(0) }, Sleep: SleepContext}
	cfg := Config{Repo: "o/r", Ref: "main", Workflows: []string{"go-build"}, Runners: []string{"hosted", "mayfly"},
		Warmup: 1, Runs: 1, Poll: time.Millisecond, RunTimeout: time.Second}
	return h, cfg
}

// TestHarnessRun checks dispatch inputs, polling until completion, timings, and failed runs.
func TestHarnessRun(t *testing.T) {
	api := &fakeAPI{polls: 2, pollErrs: 1, jobLag: 2, conclusion: func(r string) string {
		if r == "mayfly" {
			return "failure"
		}
		return "success"
	}}
	h, cfg := testHarness(api)

	var recs []Record
	if err := h.Run(context.Background(), cfg, func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("got %d records, want 4", len(recs))
	}
	if api.dispatched[1]["file"] != "go-build.yml" || api.dispatched[1]["runner"] != "mayfly" {
		t.Errorf("second dispatch = %v", api.dispatched[1])
	}
	hosted, mayfly := recs[2], recs[3]
	if want := (Timing{Queue: 8 * time.Second, FirstStep: 12 * time.Second, Total: 40 * time.Second}); hosted.Timing != want {
		t.Errorf("hosted timing = %+v, want %+v", hosted.Timing, want)
	}
	if hosted.Warmup || !recs[0].Warmup || hosted.RunnerName != "hosted-1" || hosted.JobID != hosted.RunID*10 {
		t.Errorf("hosted record = %+v", hosted)
	}
	if mayfly.Conclusion != "failure" || mayfly.Timing != (Timing{}) || mayfly.Counts() {
		t.Errorf("failed run should be recorded without timings and not count: %+v", mayfly)
	}
}

// TestHarnessOneTimeout checks that a single stuck run is cancelled, recorded as timed_out, and the benchmark goes on.
func TestHarnessOneTimeout(t *testing.T) {
	api := &fakeAPI{stuck: func(id int64) bool { return id == 2 }, conclusion: func(string) string { return "success" }}
	h, cfg := testHarness(api)
	cfg.RunTimeout = 20 * time.Millisecond

	var recs []Record
	if err := h.Run(context.Background(), cfg, func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[1].Conclusion != ConclusionTimedOut || recs[1].Counts() || recs[2].Conclusion != ConclusionSuccess {
		t.Errorf("records = %+v", recs)
	}
	if len(api.cancelled) != 1 || api.cancelled[0] != 2 {
		t.Errorf("cancelled = %v, want [2]", api.cancelled)
	}
}

// TestHarnessTimeoutsInARow checks that two stuck runs in a row stop the benchmark, keeping both rows.
func TestHarnessTimeoutsInARow(t *testing.T) {
	h, cfg := testHarness(&fakeAPI{stuck: func(int64) bool { return true }, conclusion: func(string) string { return "success" }})
	cfg.RunTimeout = 20 * time.Millisecond
	var recs []Record
	err := h.Run(context.Background(), cfg, func(r Record) error { recs = append(recs, r); return nil })
	if !errors.Is(err, errRunTimedOut) || !strings.Contains(err.Error(), "2 runs in a row") {
		t.Errorf("err = %v, want two timeouts in a row", err)
	}
	if len(recs) != 2 {
		t.Errorf("got %d records, want both timed-out rows", len(recs))
	}
}

// TestHarnessJobNeverCompletes checks that a job stuck behind a completed run times out instead of failing at once.
func TestHarnessJobNeverCompletes(t *testing.T) {
	h, cfg := testHarness(&fakeAPI{jobLag: 1 << 30, conclusion: func(string) string { return "success" }})
	cfg.RunTimeout = 20 * time.Millisecond
	var recs []Record
	err := h.Run(context.Background(), cfg, func(r Record) error { recs = append(recs, r); return nil })
	if !errors.Is(err, errRunTimedOut) || len(recs) != 2 || recs[0].Conclusion != ConclusionTimedOut {
		t.Errorf("err = %v, records = %+v, want timeouts", err, recs)
	}
}

// TestHarnessEmitError checks that a failure to record stops the benchmark.
func TestHarnessEmitError(t *testing.T) {
	h, cfg := testHarness(&fakeAPI{conclusion: func(string) string { return "success" }})
	err := h.Run(context.Background(), cfg, func(Record) error { return errors.New("disk full") })
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want the emit error", err)
	}
}
