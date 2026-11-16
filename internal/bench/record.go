package bench

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

// ConclusionSuccess is the conclusion of a run whose timings count.
const ConclusionSuccess = "success"

// firstDataLine is the file line of the first row after the header, for error messages.
const firstDataLine = 2

// header is the CSV header; Record.row and parseRecord follow the same column order.
var header = []string{
	"dispatched_at", "workflow", "runner", "iteration", "warmup", "run_id", "job_id",
	"runner_name", "conclusion", "queue_s", "first_step_s", "total_s",
}

// Record is one measured job, one CSV row.
type Record struct {
	DispatchedAt time.Time
	Item
	RunID      int64
	JobID      int64
	RunnerName string
	Conclusion string
	Timing
}

// Counts reports whether the record belongs in a summary: a measured run that succeeded.
func (r Record) Counts() bool {
	return !r.Warmup && r.Conclusion == ConclusionSuccess
}

// Writer writes records as CSV, flushing each row so an interrupted benchmark keeps what it measured.
type Writer struct {
	w *csv.Writer
}

// NewWriter writes the CSV header to w and returns a Writer for the rows.
func NewWriter(w io.Writer) (*Writer, error) {
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return nil, err
	}
	cw.Flush()
	return &Writer{w: cw}, cw.Error()
}

// Write appends r and flushes it.
func (w *Writer) Write(r Record) error {
	if err := w.w.Write(r.row()); err != nil {
		return err
	}
	w.w.Flush()
	return w.w.Error()
}

// row renders r in header order; durations are whole seconds, the resolution of GitHub's timestamps.
func (r Record) row() []string {
	secs := func(d time.Duration) string { return strconv.FormatInt(int64(d/time.Second), 10) }
	return []string{
		r.DispatchedAt.UTC().Format(time.RFC3339), r.Workflow, r.Runner, strconv.Itoa(r.Iteration),
		strconv.FormatBool(r.Warmup), strconv.FormatInt(r.RunID, 10), strconv.FormatInt(r.JobID, 10),
		r.RunnerName, r.Conclusion, secs(r.Queue), secs(r.FirstStep), secs(r.Total),
	}
}

// ReadRecords parses a CSV written by Writer.
func ReadRecords(r io.Reader) ([]Record, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(header)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("empty file: no CSV header")
	}
	for i, col := range header {
		if rows[0][i] != col {
			return nil, fmt.Errorf("column %d is %q, want %q", i+1, rows[0][i], col)
		}
	}
	out := make([]Record, 0, len(rows)-1)
	for i, row := range rows[1:] {
		rec, err := parseRecord(row)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+firstDataLine, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// parseRecord turns one CSV row in header order back into a Record.
func parseRecord(row []string) (Record, error) {
	var (
		r    Record
		errs []error
	)
	parseInt := func(s string) int64 {
		v, err := strconv.ParseInt(s, 10, 64)
		errs = append(errs, err)
		return v
	}
	secs := func(s string) time.Duration { return time.Duration(parseInt(s)) * time.Second }

	t, err := time.Parse(time.RFC3339, row[0])
	errs = append(errs, err)
	w, err := strconv.ParseBool(row[4])
	errs = append(errs, err)
	r = Record{
		DispatchedAt: t,
		Item:         Item{Workflow: row[1], Runner: row[2], Iteration: int(parseInt(row[3])), Warmup: w},
		RunID:        parseInt(row[5]),
		JobID:        parseInt(row[6]),
		RunnerName:   row[7],
		Conclusion:   row[8],
		Timing:       Timing{Queue: secs(row[9]), FirstStep: secs(row[10]), Total: secs(row[11])},
	}
	return r, errors.Join(errs...)
}
