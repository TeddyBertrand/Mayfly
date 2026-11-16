package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// csvHead is the header bench writes, repeated here so the test catches an accidental change.
const csvHead = "dispatched_at,workflow,runner,iteration,warmup,run_id,job_id,runner_name,conclusion,queue_s,first_step_s,total_s\n"

// writeCSV writes rows under the header into a temp file and returns its path.
func writeCSV(t *testing.T, name string, rows ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(csvHead+strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// runArgs runs the command with args and returns its stdout and error.
func runArgs(args ...string) (string, error) {
	var out bytes.Buffer
	err := run(context.Background(), args, &out, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return out.String(), err
}

// TestCompare checks the exit verdicts of compare on files inside and outside the tolerance.
func TestCompare(t *testing.T) {
	a := writeCSV(t, "a.csv", "2026-11-16T15:21:00Z,go-build,mayfly,2,false,1,2,m,success,8,12,40")
	inside := writeCSV(t, "b.csv", "2026-11-16T16:21:00Z,go-build,mayfly,2,false,3,4,m,success,8,12,43")
	outside := writeCSV(t, "c.csv", "2026-11-16T16:21:00Z,go-build,mayfly,2,false,3,4,m,success,8,12,50")

	if out, err := runArgs("compare", a, inside); err != nil || !strings.Contains(out, "| yes |") {
		t.Errorf("inside: err = %v, out:\n%s", err, out)
	}
	if _, err := runArgs("compare", a, outside); !errors.Is(err, errOutsideTolerance) {
		t.Errorf("outside: err = %v, want errOutsideTolerance", err)
	}
	if _, err := runArgs("compare", "-tolerance", "0.3", a, outside); err != nil {
		t.Errorf("wider tolerance: err = %v", err)
	}
}

// TestSummary checks that summary prints one row per cell.
func TestSummary(t *testing.T) {
	p := writeCSV(t, "a.csv",
		"2026-11-16T15:21:00Z,go-build,mayfly,0,true,1,2,m,success,8,12,99",
		"2026-11-16T15:22:00Z,go-build,mayfly,1,false,3,4,m,success,8,12,40")
	out, err := runArgs("summary", p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| go-build | mayfly | 1 | 8s (8s) | 12s (12s) | 40s (40s) | 40s-40s | 0 |") {
		t.Errorf("summary:\n%s", out)
	}
}

// TestRunErrors checks argument validation before any GitHub call.
func TestRunErrors(t *testing.T) {
	existing := writeCSV(t, "old.csv")
	t.Setenv(tokenEnv, "")
	tests := []struct {
		name string
		args []string
	}{
		{"no subcommand", nil},
		{"unknown subcommand", []string{"bogus"}},
		{"run without -out", []string{"run"}},
		{"run with zero runs", []string{"run", "-out", "x.csv", "-runs", "0"}},
		{"run without token", []string{"run", "-out", filepath.Join(t.TempDir(), "new.csv")}},
		{"summary without file", []string{"summary"}},
		{"compare with one file", []string{"compare", existing}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runArgs(tt.args...); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// TestRunRefusesOverwrite checks that an existing results file is never replaced.
func TestRunRefusesOverwrite(t *testing.T) {
	existing := writeCSV(t, "old.csv", "2026-11-16T15:21:00Z,go-build,mayfly,1,false,1,2,m,success,8,12,40")
	before, _ := os.ReadFile(existing)
	t.Setenv(tokenEnv, "x")
	if _, err := runArgs("run", "-out", existing, "-api", "http://127.0.0.1:1"); err == nil {
		t.Error("want an error for an existing file")
	}
	if after, _ := os.ReadFile(existing); !bytes.Equal(before, after) {
		t.Error("existing file changed")
	}
}

// TestSplitList checks trimming and empty items.
func TestSplitList(t *testing.T) {
	if got, want := splitList(" hosted, mayfly,,classic "), []string{"hosted", "mayfly", "classic"}; !reflect.DeepEqual(got, want) {
		t.Errorf("splitList = %v, want %v", got, want)
	}
}
