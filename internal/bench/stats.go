// Package bench runs the demo workflows on several runner types and summarizes GitHub's job timings.
package bench

import (
	"math"
	"slices"
	"time"
)

const (
	// p90Rank is the percentile the summary reports next to the median.
	p90Rank = 0.9
	// middlePair is how many middle values an even count averages for its median.
	middlePair = 2
)

// Summary describes one set of durations.
type Summary struct {
	N      int
	Median time.Duration
	P90    time.Duration
	Min    time.Duration
	Max    time.Duration
}

// Summarize returns the median, nearest-rank p90, min and max of ds. An empty ds gives a zero Summary.
func Summarize(ds []time.Duration) Summary {
	if len(ds) == 0 {
		return Summary{}
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	return Summary{N: len(s), Median: median(s), P90: nearestRank(s, p90Rank), Min: s[0], Max: s[len(s)-1]}
}

// median returns the middle of sorted, or the mean of the two middle values when its length is even.
func median(sorted []time.Duration) time.Duration {
	mid := len(sorted) / middlePair
	if len(sorted)%middlePair == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / middlePair
}

// nearestRank returns the smallest value of sorted with at least p of the values at or below it.
func nearestRank(sorted []time.Duration, p float64) time.Duration {
	rank := int(math.Ceil(p * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}

// RelDiff returns |b-a|/a, the change from a to b as a fraction of a. It is +Inf when a is zero and b is not.
func RelDiff(a, b time.Duration) float64 {
	if a == 0 {
		if b == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return math.Abs(float64(b-a)) / float64(a)
}
