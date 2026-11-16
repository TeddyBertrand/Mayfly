package bench

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestRecordRoundTrip checks that what Writer writes, ReadRecords reads back unchanged.
func TestRecordRoundTrip(t *testing.T) {
	recs := []Record{
		{DispatchedAt: at(0), Item: Item{Iteration: 0, Workflow: "go-build", Runner: "mayfly", Warmup: true},
			RunID: 43895415425, JobID: 133397528970, RunnerName: "mayfly-133397528970-38a469", Conclusion: "success",
			Timing: Timing{Queue: 8 * time.Second, FirstStep: 12 * time.Second, Total: 40 * time.Second}},
		{DispatchedAt: at(60), Item: Item{Iteration: 1, Workflow: "npm-test", Runner: "hosted"},
			RunID: 2, JobID: 3, RunnerName: "GitHub Actions 1", Conclusion: "failure"},
	}
	var buf bytes.Buffer
	w, err := NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadRecords(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, recs) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, recs)
	}
}

// TestReadRecordsErrors checks that a wrong header, a bad number or a short row is rejected.
func TestReadRecordsErrors(t *testing.T) {
	head := strings.Join(header, ",") + "\n"
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"wrong header", strings.Replace(head, "queue_s", "queue", 1)},
		{"bad number", head + "2026-11-16T15:21:00Z,go-build,mayfly,x,false,1,2,n,success,8,12,40\n"},
		{"bad time", head + "yesterday,go-build,mayfly,0,false,1,2,n,success,8,12,40\n"},
		{"short row", head + "2026-11-16T15:21:00Z,go-build\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReadRecords(strings.NewReader(tt.in)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// TestCounts checks that warm-up and failed runs stay out of summaries.
func TestCounts(t *testing.T) {
	tests := []struct {
		r    Record
		want bool
	}{
		{Record{Conclusion: "success"}, true},
		{Record{Conclusion: "failure"}, false},
		{Record{Item: Item{Warmup: true}, Conclusion: "success"}, false},
	}
	for _, tt := range tests {
		if got := tt.r.Counts(); got != tt.want {
			t.Errorf("Counts(%+v) = %v, want %v", tt.r, got, tt.want)
		}
	}
}
