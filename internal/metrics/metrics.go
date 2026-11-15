// Package metrics defines every Prometheus metric Mayfly exposes, so names, units and buckets live in one place (decision 020).
// docs/metrics.md lists them.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

// namespace prefixes every metric name, so Mayfly's series are easy to find among the Go runtime ones.
const namespace = "mayfly"

// subsystemReaper groups the reaper's metrics under mayfly_reaper_.
const subsystemReaper = "reaper"

// Values of the op label on mayfly_retries_total: the operation that was retried.
const (
	OpJITConfig   = "jit_config"
	OpWorkerStart = "worker_start"
)

// Values of the stage label on mayfly_provision_failures_total: where provisioning gave up.
const (
	StageJITConfig = "jit_config"
	StageWorker    = "worker"
)

// knownConclusions are GitHub's job conclusions. Each starts at 0 so increase() also counts the first job of a kind:
// a series that first appears at 1 has no earlier sample to grow from.
var knownConclusions = []string{"success", "failure", "cancelled", "skipped", "timed_out", "neutral", "action_required"}

// waitBuckets cover queue and boot times, in seconds: a few seconds when warm, minutes on a cold image pull.
var waitBuckets = []float64{1, 2, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 300, 600}

// jobBuckets cover job durations, in seconds, from a short lint job up to GitHub's 6h job limit.
var jobBuckets = []float64{10, 30, 60, 120, 300, 600, 1200, 1800, 3600, 7200, 14400, 21600}

// Mayfly holds the metrics of the mayfly process: the receiver, provisioner and Docker backend.
type Mayfly struct {
	QueueSeconds      prometheus.Histogram
	BootSeconds       prometheus.Histogram
	JobSeconds        prometheus.Histogram
	WorkersAlive      prometheus.Gauge
	JobsWaiting       prometheus.Gauge
	Jobs              *prometheus.CounterVec
	ProvisionFailures *prometheus.CounterVec
	Retries           *prometheus.CounterVec
}

// NewMayfly creates the mayfly metrics and registers them with reg; a nil reg leaves them unregistered, for tests.
func NewMayfly(reg prometheus.Registerer) *Mayfly {
	m := newMayfly(promauto.With(reg))
	Preset(m.Jobs, knownConclusions...)
	Preset(m.ProvisionFailures, StageJITConfig, StageWorker)
	Preset(m.Retries, OpJITConfig, OpWorkerStart)
	return m
}

// Preset creates the series of v for each label value at 0, so dashboards see them before the first event.
func Preset(v *prometheus.CounterVec, values ...string) {
	for _, val := range values {
		v.WithLabelValues(val)
	}
}

// newMayfly builds the mayfly metrics with f.
func newMayfly(f promauto.Factory) *Mayfly {
	return &Mayfly{
		QueueSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "job_queue_seconds",
			Help:    "Time a job waited for a runner, from GitHub's created_at to started_at.",
			Buckets: waitBuckets,
		}),
		BootSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "worker_boot_seconds",
			Help:    "Time from mayfly accepting a queued job to its runner picking up a job, on mayfly's clock.",
			Buckets: waitBuckets,
		}),
		JobSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "job_duration_seconds",
			Help:    "Time a job ran, from GitHub's started_at to completed_at.",
			Buckets: jobBuckets,
		}),
		WorkersAlive: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "workers_alive",
			Help: "Workers started by this process and not yet gone.",
		}),
		JobsWaiting: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "jobs_waiting",
			Help: "Accepted jobs waiting for a free worker slot.",
		}),
		Jobs: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "jobs_total",
			Help: "Completed jobs, by GitHub conclusion.",
		}, []string{logkey.Conclusion}),
		ProvisionFailures: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "provision_failures_total",
			Help: "Jobs mayfly gave up provisioning, by the stage that failed.",
		}, []string{logkey.Stage}),
		Retries: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "retries_total",
			Help: "Retried attempts of a failed call, by operation.",
		}, []string{logkey.Op}),
	}
}

// Reaper holds the metrics of the reaper process.
type Reaper struct {
	WorkersRemoved       *prometheus.CounterVec
	RegistrationsRemoved prometheus.Counter
	StuckJobs            *prometheus.CounterVec
	SweepFailures        prometheus.Counter
	LastSweep            prometheus.Gauge
}

// NewReaper creates the reaper metrics and registers them with reg; a nil reg leaves them unregistered, for tests.
func NewReaper(reg prometheus.Registerer) *Reaper {
	f := promauto.With(reg)
	return &Reaper{
		WorkersRemoved: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: subsystemReaper, Name: "workers_removed_total",
			Help: "Orphaned workers removed, by reason.",
		}, []string{logkey.Reason}),
		RegistrationsRemoved: f.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: subsystemReaper, Name: "registrations_removed_total",
			Help: "Stale runner registrations deleted on GitHub.",
		}),
		StuckJobs: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: subsystemReaper, Name: "stuck_jobs_total",
			Help: "Jobs found stuck in queued, each counted once, by reason.",
		}, []string{logkey.Reason}),
		SweepFailures: f.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: subsystemReaper, Name: "sweep_failures_total",
			Help: "Sweeps that could not list workers and did nothing.",
		}),
		LastSweep: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystemReaper, Name: "last_sweep_timestamp_seconds",
			Help: "Unix time of the last sweep that listed workers.",
		}),
	}
}

// NewRegistry returns a registry holding the Go runtime and process collectors, for one process's own metrics.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// Handler serves the metrics gathered from g in the Prometheus text format.
func Handler(g prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{})
}
