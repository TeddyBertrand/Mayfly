package provisioner

import (
	"testing"
)

// TestTransitions checks every (from, to) pair against the documented state machine.
func TestTransitions(t *testing.T) {
	allowed := map[[2]State]bool{
		{Waiting, Provisioning}: true,
		{Waiting, Dropped}:      true,
		{Provisioning, Ready}:   true,
		{Provisioning, Dropped}: true,
		{Ready, Running}:        true,
		{Ready, Done}:           true,
		{Ready, Dropped}:        true,
		{Running, Done}:         true,
	}
	for from := Waiting; from <= Dropped; from++ {
		for to := Waiting; to <= Dropped; to++ {
			if got, want := canTransition(from, to), allowed[[2]State{from, to}]; got != want {
				t.Errorf("canTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestTarget checks the state each workflow_job action asks for.
func TestTarget(t *testing.T) {
	tests := []struct {
		action, conclusion string
		cur, want          State
	}{
		{"in_progress", "", Ready, Running},
		{"completed", "success", Running, Done},
		{"completed", "cancelled", Running, Done},
		{"completed", "cancelled", Waiting, Dropped},
		{"completed", "cancelled", Provisioning, Dropped},
		{"completed", "cancelled", Ready, Dropped},
		{"completed", "success", Ready, Done},
		{"completed", "failure", Waiting, Done},
	}
	for _, tt := range tests {
		if got := target(tt.action, tt.conclusion, tt.cur); got != tt.want {
			t.Errorf("target(%q, %q, %s) = %s, want %s", tt.action, tt.conclusion, tt.cur, got, tt.want)
		}
	}
}

// TestStateString checks names for known states and a fallback for unknown ones.
func TestStateString(t *testing.T) {
	if got := Running.String(); got != "running" {
		t.Errorf("Running.String() = %q", got)
	}
	if got := State(42).String(); got != "state(42)" {
		t.Errorf("State(42).String() = %q", got)
	}
}
