package bench

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
)

// setupJobStep is the step the runner adds before the workflow's own steps.
const setupJobStep = "Set up job"

// Timing holds the three durations measured for one job, all from the job's created_at.
type Timing struct {
	// Queue ends when a runner picked the job up (started_at).
	Queue time.Duration
	// FirstStep ends when the first workflow step after "Set up job" started.
	FirstStep time.Duration
	// Total ends when the job completed.
	Total time.Duration
}

// Measure computes the Timing of a completed job from GitHub's timestamps.
func Measure(job github.Job) (Timing, error) {
	if job.Status != github.RunStatusCompleted {
		return Timing{}, fmt.Errorf("job %d is %s, not completed", job.ID, job.Status)
	}
	if job.CreatedAt.IsZero() || job.StartedAt.IsZero() || job.CompletedAt.IsZero() {
		return Timing{}, fmt.Errorf("job %d is missing a timestamp", job.ID)
	}
	first, err := firstWorkflowStep(job.Steps)
	if err != nil {
		return Timing{}, fmt.Errorf("job %d: %w", job.ID, err)
	}
	return Timing{
		Queue:     job.StartedAt.Sub(job.CreatedAt),
		FirstStep: first.Sub(job.CreatedAt),
		Total:     job.CompletedAt.Sub(job.CreatedAt),
	}, nil
}

// firstWorkflowStep returns when the first step after the runner's own setup started.
func firstWorkflowStep(steps []github.Step) (time.Time, error) {
	sorted := slices.Clone(steps)
	slices.SortFunc(sorted, func(a, b github.Step) int { return a.Number - b.Number })
	for _, s := range sorted {
		if s.Name != setupJobStep && !s.StartedAt.IsZero() {
			return s.StartedAt, nil
		}
	}
	return time.Time{}, errors.New("no workflow step started")
}
