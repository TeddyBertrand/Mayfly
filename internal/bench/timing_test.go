package bench

import (
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
)

// at returns a fixed base time plus s seconds.
func at(s int) time.Time {
	return time.Date(2026, 11, 16, 15, 21, 0, 0, time.UTC).Add(time.Duration(s) * time.Second)
}

// TestMeasure checks the three durations on a real-shaped job and the error cases.
func TestMeasure(t *testing.T) {
	steps := []github.Step{
		{Number: 2, Name: "Run actions/checkout@v4", StartedAt: at(12)},
		{Number: 1, Name: "Set up job", StartedAt: at(10)},
		{Number: 3, Name: "Run actions/setup-go@v5", StartedAt: at(13)},
	}
	done := github.Job{ID: 1, Status: github.RunStatusCompleted, CreatedAt: at(0), StartedAt: at(8), CompletedAt: at(40), Steps: steps}

	tests := []struct {
		name    string
		job     func(github.Job) github.Job
		want    Timing
		wantErr bool
	}{
		{"completed job, steps out of order", func(j github.Job) github.Job { return j },
			Timing{Queue: 8 * time.Second, FirstStep: 12 * time.Second, Total: 40 * time.Second}, false},
		{"still running", func(j github.Job) github.Job { j.Status = "in_progress"; return j }, Timing{}, true},
		{"never started", func(j github.Job) github.Job { j.StartedAt = time.Time{}; return j }, Timing{}, true},
		{"failed during setup", func(j github.Job) github.Job { j.Steps = steps[1:2]; return j }, Timing{}, true},
		{"skipped first step", func(j github.Job) github.Job {
			j.Steps = []github.Step{steps[1], {Number: 2, Name: "Run x"}, steps[2]}
			return j
		}, Timing{Queue: 8 * time.Second, FirstStep: 13 * time.Second, Total: 40 * time.Second}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Measure(tt.job(done))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Measure = %+v, want %+v", got, tt.want)
			}
		})
	}
}
