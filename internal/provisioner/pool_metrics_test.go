package provisioner

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// wantRequests fails the test unless testJob's repository counted exactly the given requests per result.
func wantRequests(t *testing.T, p *Provisioner, want map[string]float64) {
	t.Helper()
	for _, result := range []string{metrics.PoolHit, metrics.PoolMissEmpty, metrics.PoolMissMax, metrics.PoolMissUnresolved} {
		if got := testutil.ToFloat64(p.metrics.PoolRequests.WithLabelValues(testJob.Repo, result)); got != want[result] {
			t.Errorf("pool requests{%s} = %v, want %v", result, got, want[result])
		}
	}
}

// TestPoolRequestResults checks each queued job counts once, as a hit or with the reason it missed.
func TestPoolRequestResults(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*pool)
		jobs  []int
		want  map[string]float64
	}{
		{name: "hit", jobs: []int{1}, want: map[string]float64{metrics.PoolHit: 1}},
		{name: "redelivered hit counts once", jobs: []int{1, 1}, want: map[string]float64{metrics.PoolHit: 1}},
		{name: "miss below the cap", jobs: []int{1, 2}, want: map[string]float64{metrics.PoolHit: 1, metrics.PoolMissEmpty: 1}},
		{name: "redelivered miss counts once", jobs: []int{1, 2, 2}, want: map[string]float64{metrics.PoolHit: 1, metrics.PoolMissEmpty: 1}},
		{
			name: "miss at the cap", setup: func(w *pool) { w.max = 1 }, jobs: []int{1, 2},
			want: map[string]float64{metrics.PoolHit: 1, metrics.PoolMissMax: 1},
		},
		{
			name: "installation unknown", setup: func(w *pool) { w.repos[repoKey(testJob.Repo)].installationID = 0 }, jobs: []int{1},
			want: map[string]float64{metrics.PoolMissUnresolved: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := idlePool("mayfly-warm-aaaaaa")
			if tt.setup != nil {
				tt.setup(p.pool)
			}
			for _, i := range tt.jobs {
				if err := p.Enqueue(jobN(i)); err != nil {
					t.Fatal(err)
				}
			}
			wantRequests(t, p, tt.want)
		})
	}
}

// TestUnpooledRepoNotCounted checks a job of a repository without a pool is not a pool request.
func TestUnpooledRepoNotCounted(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa")
	job := jobN(1)
	job.Repo = "octo/other"
	if err := p.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	wantRequests(t, p, nil)
	if n := testutil.CollectAndCount(p.metrics.PoolRequests); n != 4 {
		t.Errorf("request series = %d, want the 4 preset for the pooled repository", n)
	}
}

// TestPoolClaimTime checks claim time runs from the reservation to in_progress, once, and only for jobs that reserved.
func TestPoolClaimTime(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa", "mayfly-warm-bbbbbb")
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return t0 }
	if err := p.Enqueue(jobN(1)); err != nil {
		t.Fatal(err)
	}

	p.now = func() time.Time { return t0.Add(2 * time.Second) }
	for range 2 {
		p.Update(workflow.ActionInProgress, inProgress(jobN(1), "mayfly-warm-aaaaaa"))
	}
	// Job 2 never reserved, so it has no start time to measure from.
	p.Update(workflow.ActionInProgress, inProgress(jobN(2), "mayfly-warm-bbbbbb"))

	if n, sum := histogram(t, p.metrics.PoolClaimSeconds); n != 1 || sum != 2 {
		t.Errorf("claim = %d values, sum %v; want 1, 2", n, sum)
	}
}

// TestPoolSizeGauges checks the gauges report the pool as it is at each scrape, through a claim.
func TestPoolSizeGauges(t *testing.T) {
	p, _, _ := idlePool("mayfly-warm-aaaaaa", "mayfly-warm-bbbbbb")
	c := metrics.NewPoolCollector(p.pool.snapshot)
	check := func(idle, busy string) {
		t.Helper()
		want := `
# HELP mayfly_pool_max_runners Cap on the warm runners alive for a pooled repository.
# TYPE mayfly_pool_max_runners gauge
mayfly_pool_max_runners{repo="octo/demo"} 10
# HELP mayfly_pool_runners Warm runners of a pooled repository, by state.
# TYPE mayfly_pool_runners gauge
mayfly_pool_runners{repo="octo/demo",state="busy"} ` + busy + `
mayfly_pool_runners{repo="octo/demo",state="idle"} ` + idle + `
mayfly_pool_runners{repo="octo/demo",state="pending"} 0
mayfly_pool_runners{repo="octo/demo",state="retiring"} 0
`
		if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
			t.Error(err)
		}
	}

	check("2", "0")
	p.Update(workflow.ActionInProgress, inProgress(jobN(1), "mayfly-warm-aaaaaa"))
	check("1", "1")
}
