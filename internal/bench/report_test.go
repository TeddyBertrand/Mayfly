package bench

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// rec builds a record of cell w/r with a total time of total seconds.
func rec(w, r string, total int, warmup bool, conclusion string) Record {
	d := time.Duration(total) * time.Second
	return Record{Item: Item{Workflow: w, Runner: r, Warmup: warmup}, Conclusion: conclusion, Timing: Timing{Queue: d / 4, FirstStep: d / 2, Total: d}}
}

// TestSummaries checks grouping order, warm-up exclusion and failure counting.
func TestSummaries(t *testing.T) {
	recs := []Record{
		rec("go", "mayfly", 999, true, "success"),
		rec("go", "mayfly", 40, false, "success"),
		rec("go", "hosted", 60, false, "success"),
		rec("go", "mayfly", 44, false, "success"),
		rec("go", "mayfly", 10, false, "failure"),
	}
	sums := Summaries(recs)
	if len(sums) != 2 || sums[0].Cell != (Cell{"go", "mayfly"}) || sums[1].Cell != (Cell{"go", "hosted"}) {
		t.Fatalf("cells = %+v", sums)
	}
	m := sums[0]
	if m.Total.N != 2 || m.Total.Median != 42*time.Second || m.Failed != 1 {
		t.Errorf("mayfly summary = %+v, want n=2 median=42s failed=1", m)
	}
	if m.Queue.Median != 10500*time.Millisecond {
		t.Errorf("queue median = %v, want 10.5s", m.Queue.Median)
	}
}

// TestWriteSummary checks one rendered row.
func TestWriteSummary(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSummary(&buf, Summaries([]Record{rec("go", "mayfly", 40, false, "success")})); err != nil {
		t.Fatal(err)
	}
	if want := "| go | mayfly | 1 | 10s (10s) | 20s (20s) | 40s (40s) | 40s-40s | 0 |"; !strings.Contains(buf.String(), want) {
		t.Errorf("summary:\n%s\nwant row %s", buf.String(), want)
	}
}

// TestCompare checks the tolerance boundary, a missing cell and a cell without counted runs.
func TestCompare(t *testing.T) {
	a := Summaries([]Record{
		rec("go", "mayfly", 40, false, "success"),
		rec("go", "hosted", 60, false, "success"),
		rec("npm", "mayfly", 20, false, "success"),
		rec("npm", "hosted", 20, false, "failure"),
	})
	b := Summaries([]Record{
		rec("go", "mayfly", 44, false, "success"),
		rec("go", "hosted", 67, false, "success"),
		rec("npm", "hosted", 20, false, "success"),
	})
	got := map[Cell]bool{}
	for _, c := range Compare(a, b, 0.10) {
		got[c.Cell] = c.OK
	}
	want := map[Cell]bool{
		{"go", "mayfly"}:  true,  // +10%, on the boundary
		{"go", "hosted"}:  false, // +11.7%
		{"npm", "mayfly"}: false, // missing in b
		{"npm", "hosted"}: false, // no counted run in a
	}
	for c, ok := range want {
		if got[c] != ok {
			t.Errorf("%v OK = %v, want %v", c, got[c], ok)
		}
	}
}

// TestWriteComparison checks the overall verdict and the rendered tolerance.
func TestWriteComparison(t *testing.T) {
	var buf bytes.Buffer
	all, err := WriteComparison(&buf, []Comparison{
		{Cell: Cell{"go", "mayfly"}, A: 40 * time.Second, B: 44 * time.Second, Diff: 0.1, OK: true},
		{Cell: Cell{"go", "hosted"}, A: 60 * time.Second, B: 67 * time.Second, Diff: 0.117, OK: false},
	}, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if all {
		t.Error("want overall failure")
	}
	for _, want := range []string{"within ±10%", "| go | mayfly | 40s | 44s | 10.0% | yes |", "| go | hosted | 60s | 67s | 11.7% | **no** |"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("comparison missing %q:\n%s", want, buf.String())
		}
	}
}
