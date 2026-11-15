// Package workflow holds the GitHub workflow job as Mayfly sees it, shared by the receiver, provisioner and reaper.
package workflow

import (
	"strings"
	"time"
)

// Action is the action of a workflow_job webhook event, as GitHub sends it.
type Action string

// The workflow_job actions Mayfly acts on. GitHub sends others, such as "waiting", which are ignored.
const (
	ActionQueued     Action = "queued"
	ActionInProgress Action = "in_progress"
	ActionCompleted  Action = "completed"
)

// ConclusionCancelled is the conclusion of a completed job that was cancelled, which may never have started.
const ConclusionCancelled = "cancelled"

// Job is the part of a workflow job the provisioner needs to start a worker and follow it.
type Job struct {
	ID             int64
	RunID          int64
	Name           string
	Repo           string
	InstallationID int64
	Labels         []string
	// RunnerName is set once a runner picks the job up (in_progress, completed).
	RunnerName string
	// Conclusion is set on completed, e.g. "success" or "cancelled".
	Conclusion string
	// CreatedAt, StartedAt and CompletedAt are GitHub's timestamps; zero when the event does not carry them.
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
}

// MatchesLabels reports whether labels contains every label in required, ignoring case as GitHub does.
func MatchesLabels(labels, required []string) bool {
	for _, want := range required {
		found := false
		for _, l := range labels {
			if strings.EqualFold(l, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
