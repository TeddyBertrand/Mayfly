// Package worker defines how Mayfly runs one job on a throwaway machine.
package worker

import (
	"context"
	"fmt"
	"log/slog"
)

// Spec describes one ephemeral worker. JITConfig is a credential and must not be logged.
type Spec struct {
	JobID      int64
	RunnerName string
	JITConfig  string
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
