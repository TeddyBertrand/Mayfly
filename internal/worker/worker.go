// Package worker defines how Mayfly runs one job on a throwaway machine.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Spec describes one ephemeral worker. JITConfig is a credential and must not be logged.
type Spec struct {
	JobID      int64
	RunnerName string
	JITConfig  string
	// Repo, InstallationID and RunnerID let the reaper look the runner up on GitHub.
	Repo           string
	InstallationID int64
	RunnerID       int64
}

// LogValue hides the JIT config so a Spec can be logged safely.
func (s Spec) LogValue() slog.Value {
	return slog.GroupValue(slog.Int64("job_id", s.JobID), slog.String("runner_name", s.RunnerName))
}

// String hides the JIT config so a Spec cannot leak through fmt.
func (s Spec) String() string {
	return fmt.Sprintf("Spec{JobID:%d RunnerName:%s JITConfig:[redacted]}", s.JobID, s.RunnerName)
}

// Backend runs a worker for spec until its single job ends, then destroys it.
// Run must leave nothing behind, whether it returns an error or not.
type Backend interface {
	Run(ctx context.Context, spec Spec) error
}

// Info is a worker found on the host, as the reaper sees it.
// Numeric fields are zero when the backend has no value for them, for example on workers from an older version.
type Info struct {
	// ID is the backend's own ID for the worker, such as a container ID.
	ID             string
	Created        time.Time
	JobID          int64
	RunnerName     string
	Owner          string
	Repo           string
	InstallationID int64
	RunnerID       int64
}

// Inventory lists and removes the workers present on the host, independently of the Backend that started them.
type Inventory interface {
	List(ctx context.Context) ([]Info, error)
	// Remove destroys worker id, stopping it if it runs. A worker already gone is not an error.
	Remove(ctx context.Context, id string) error
}
