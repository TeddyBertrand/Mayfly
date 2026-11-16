package bench

import (
	"fmt"
	"io"
	"slices"
	"time"
)

// Cell groups one workflow on one runner type.
type Cell struct {
	Workflow string
	Runner   string
}

// CellSummary is the summary of one cell's counted runs, and how many runs did not succeed.
type CellSummary struct {
	Cell
	Queue     Summary
	FirstStep Summary
	Total     Summary
	Failed    int
}

// Summaries groups records by cell, in the order cells first appear, and summarizes each one.
// Warm-up runs are ignored; measured runs that did not succeed are counted in Failed.
func Summaries(recs []Record) []CellSummary {
	var order []Cell
	byCell := map[Cell][]Record{}
	for _, r := range recs {
		c := Cell{r.Workflow, r.Runner}
		if _, ok := byCell[c]; !ok {
			order = append(order, c)
		}
		byCell[c] = append(byCell[c], r)
	}
	out := make([]CellSummary, 0, len(order))
	for _, c := range order {
		var q, f, tot []time.Duration
		failed := 0
		for _, r := range byCell[c] {
			switch {
			case r.Warmup:
			case r.Counts():
				q, f, tot = append(q, r.Queue), append(f, r.FirstStep), append(tot, r.Total)
			default:
				failed++
			}
		}
		out = append(out, CellSummary{Cell: c, Queue: Summarize(q), FirstStep: Summarize(f), Total: Summarize(tot), Failed: failed})
	}
	return out
}

// WriteSummary writes the summaries as a Markdown table, in seconds.
func WriteSummary(w io.Writer, sums []CellSummary) error {
	if _, err := fmt.Fprintln(w, "| workflow | runner | n | queue median (p90) | first step median (p90) | total median (p90) | total min-max | failed |\n|---|---|---|---|---|---|---|---|"); err != nil {
		return err
	}
	for _, s := range sums {
		if _, err := fmt.Fprintf(w, "| %s | %s | %d | %s (%s) | %s (%s) | %s (%s) | %s-%s | %d |\n",
			s.Workflow, s.Runner, s.Total.N,
			fmtSecs(s.Queue.Median), fmtSecs(s.Queue.P90),
			fmtSecs(s.FirstStep.Median), fmtSecs(s.FirstStep.P90),
			fmtSecs(s.Total.Median), fmtSecs(s.Total.P90),
			fmtSecs(s.Total.Min), fmtSecs(s.Total.Max), s.Failed); err != nil {
			return err
		}
	}
	return nil
}

// fmtSecs renders d in seconds; only an even-count median can carry a half second.
func fmtSecs(d time.Duration) string {
	return fmt.Sprintf("%gs", d.Seconds())
}

// Comparison is the change of one cell's median total time between two benchmark repetitions.
type Comparison struct {
	Cell
	A, B time.Duration
	Diff float64
	OK   bool
}

// Compare matches the cells of a and b and checks that each median total time changed by at most tolerance.
// A cell present on one side only, or without counted runs, fails.
func Compare(a, b []CellSummary, tolerance float64) []Comparison {
	find := func(sums []CellSummary, c Cell) (CellSummary, bool) {
		i := slices.IndexFunc(sums, func(s CellSummary) bool { return s.Cell == c })
		if i < 0 {
			return CellSummary{}, false
		}
		return sums[i], true
	}
	cells := make([]Cell, 0, len(a)+len(b))
	for _, s := range append(slices.Clone(a), b...) {
		if !slices.Contains(cells, s.Cell) {
			cells = append(cells, s.Cell)
		}
	}
	out := make([]Comparison, 0, len(cells))
	for _, c := range cells {
		sa, okA := find(a, c)
		sb, okB := find(b, c)
		cmp := Comparison{Cell: c, A: sa.Total.Median, B: sb.Total.Median}
		cmp.Diff = RelDiff(cmp.A, cmp.B)
		cmp.OK = okA && okB && sa.Total.N > 0 && sb.Total.N > 0 && cmp.Diff <= tolerance
		out = append(out, cmp)
	}
	return out
}

// WriteComparison writes the comparisons as a Markdown table and reports whether every cell passed.
func WriteComparison(w io.Writer, cmps []Comparison, tolerance float64) (bool, error) {
	if _, err := fmt.Fprintf(w, "| workflow | runner | median total A | median total B | change | within ±%g%% |\n|---|---|---|---|---|---|\n", tolerance*percent); err != nil {
		return false, err
	}
	all := true
	for _, c := range cmps {
		verdict := "yes"
		if !c.OK {
			verdict, all = "**no**", false
		}
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %.1f%% | %s |\n", c.Workflow, c.Runner, fmtSecs(c.A), fmtSecs(c.B), c.Diff*percent, verdict); err != nil {
			return false, err
		}
	}
	return all, nil
}

// percent turns a fraction into a percentage.
const percent = 100
