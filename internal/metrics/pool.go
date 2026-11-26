package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

// subsystemPool groups the warm pool's metrics under mayfly_pool_.
const subsystemPool = "pool"

// Values of the result label on mayfly_pool_requests_total: whether a queued job got a warm runner, and if not, why.
const (
	PoolHit = "hit"
	// PoolMissEmpty means no runner was free although the repository is below its cap.
	PoolMissEmpty = "miss_empty"
	// PoolMissMax means no runner was free and the repository is at its cap.
	PoolMissMax = "miss_max"
	// PoolMissUnresolved means the repository's installation is not known, so it has no warm runners.
	PoolMissUnresolved = "miss_unresolved"
)

// Values of the state label on mayfly_pool_runners.
const (
	poolPending  = "pending"
	poolIdle     = "idle"
	poolBusy     = "busy"
	poolRetiring = "retiring"
)

// claimBuckets cover warm claim times, in seconds, finer than waitBuckets because nothing has to boot.
var claimBuckets = []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 3, 5, 10, 30, 60}

var (
	poolRunnersDesc = prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystemPool, "runners"),
		"Warm runners of a pooled repository, by state.", []string{logkey.Repo, logkey.State}, nil)
	poolMaxDesc = prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystemPool, "max_runners"),
		"Cap on the warm runners alive for a pooled repository.", []string{logkey.Repo}, nil)
)

// PoolRepo is the warm pool of one repository at the time of a scrape.
type PoolRepo struct {
	Repo                          string
	Pending, Idle, Busy, Retiring int
	Max                           int
}

// poolCollector reads the pool at each scrape, so its gauges cannot drift from the pool's state.
type poolCollector struct {
	snapshot func() []PoolRepo
}

// NewPoolCollector returns the pool size gauges, filled from snapshot at each scrape.
// snapshot must be safe to call from any goroutine.
func NewPoolCollector(snapshot func() []PoolRepo) prometheus.Collector {
	return poolCollector{snapshot: snapshot}
}

// Describe implements prometheus.Collector.
func (c poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- poolRunnersDesc
	ch <- poolMaxDesc
}

// Collect implements prometheus.Collector.
func (c poolCollector) Collect(ch chan<- prometheus.Metric) {
	for _, r := range c.snapshot() {
		for state, n := range map[string]int{poolPending: r.Pending, poolIdle: r.Idle, poolBusy: r.Busy, poolRetiring: r.Retiring} {
			ch <- prometheus.MustNewConstMetric(poolRunnersDesc, prometheus.GaugeValue, float64(n), r.Repo, state)
		}
		ch <- prometheus.MustNewConstMetric(poolMaxDesc, prometheus.GaugeValue, float64(r.Max), r.Repo)
	}
}

// RegisterPool registers the pool size gauges read from snapshot. It does nothing on unregistered metrics.
// It must be called at most once, since a registry refuses a second collector of the same metrics.
func (m *Mayfly) RegisterPool(snapshot func() []PoolRepo) {
	if m.reg != nil {
		m.reg.MustRegister(NewPoolCollector(snapshot))
	}
}

// PresetPool creates the request series of repo at 0, one per result.
func (m *Mayfly) PresetPool(repo string) {
	for _, result := range []string{PoolHit, PoolMissEmpty, PoolMissMax, PoolMissUnresolved} {
		m.PoolRequests.WithLabelValues(repo, result)
	}
}
