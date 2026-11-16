package bench

import (
	"math"
	"testing"
	"time"
)

// secs turns whole seconds into durations, to keep the tables short.
func secs(v ...int) []time.Duration {
	out := make([]time.Duration, len(v))
	for i, s := range v {
		out[i] = time.Duration(s) * time.Second
	}
	return out
}

// TestSummarize checks median, p90, min and max on odd, even, unsorted and tiny inputs.
func TestSummarize(t *testing.T) {
	tests := []struct {
		name string
		in   []time.Duration
		want Summary
	}{
		{"empty", nil, Summary{}},
		{"one value", secs(7), Summary{N: 1, Median: 7 * time.Second, P90: 7 * time.Second, Min: 7 * time.Second, Max: 7 * time.Second}},
		{"odd count, unsorted", secs(30, 10, 20), Summary{N: 3, Median: 20 * time.Second, P90: 30 * time.Second, Min: 10 * time.Second, Max: 30 * time.Second}},
		{"even count takes the mean of the middle two", secs(40, 10, 30, 20), Summary{N: 4, Median: 25 * time.Second, P90: 40 * time.Second, Min: 10 * time.Second, Max: 40 * time.Second}},
		{"ten values: p90 is the ninth", secs(1, 2, 3, 4, 5, 6, 7, 8, 9, 100), Summary{N: 10, Median: 5500 * time.Millisecond, P90: 9 * time.Second, Min: time.Second, Max: 100 * time.Second}},
		{"an outlier moves max, not the median", secs(10, 10, 11, 11, 300), Summary{N: 5, Median: 11 * time.Second, P90: 300 * time.Second, Min: 10 * time.Second, Max: 300 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summarize(tt.in); got != tt.want {
				t.Errorf("Summarize(%v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestSummarizeKeepsInput checks that sorting works on a copy, so the caller's order is untouched.
func TestSummarizeKeepsInput(t *testing.T) {
	in := secs(3, 1, 2)
	Summarize(in)
	if in[0] != 3*time.Second {
		t.Errorf("input reordered: %v", in)
	}
}

// TestRelDiff checks the relative change between two medians, including a zero baseline.
func TestRelDiff(t *testing.T) {
	tests := []struct {
		a, b time.Duration
		want float64
	}{
		{10 * time.Second, 10 * time.Second, 0},
		{10 * time.Second, 11 * time.Second, 0.1},
		{10 * time.Second, 9 * time.Second, 0.1},
		{40 * time.Second, 50 * time.Second, 0.25},
		{0, 0, 0},
		{0, time.Second, math.Inf(1)},
	}
	for _, tt := range tests {
		if got := RelDiff(tt.a, tt.b); math.Abs(got-tt.want) > 1e-9 && got != tt.want {
			t.Errorf("RelDiff(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
