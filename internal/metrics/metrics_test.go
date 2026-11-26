package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerServesEveryMetric checks one registry holds both processes' metrics without a name clash,
// and /metrics serves them next to the Go runtime metrics.
func TestHandlerServesEveryMetric(t *testing.T) {
	reg := NewRegistry()
	m := NewMayfly(reg)
	NewReaper(reg)
	m.Jobs.WithLabelValues("success").Inc()
	m.PresetPool("octo/demo")
	m.RegisterPool(func() []PoolRepo { return []PoolRepo{{Repo: "octo/demo", Idle: 1, Max: 2}} })

	rec := httptest.NewRecorder()
	Handler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"go_goroutines",
		"mayfly_job_queue_seconds_bucket",
		"mayfly_worker_boot_seconds_bucket",
		"mayfly_job_duration_seconds_bucket",
		"mayfly_workers_alive",
		"mayfly_jobs_waiting",
		`mayfly_jobs_total{conclusion="success"} 1`,
		`mayfly_jobs_total{conclusion="failure"} 0`,
		`mayfly_provision_failures_total{stage="worker"} 0`,
		`mayfly_retries_total{op="jit_config"} 0`,
		`mayfly_pool_runners{repo="octo/demo",state="idle"} 1`,
		`mayfly_pool_max_runners{repo="octo/demo"} 2`,
		`mayfly_pool_requests_total{repo="octo/demo",result="miss_max"} 0`,
		"mayfly_pool_claim_seconds_bucket",
		"mayfly_reaper_registrations_removed_total",
		"mayfly_reaper_sweep_failures_total",
		"mayfly_reaper_last_sweep_timestamp_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
}

// TestNilRegistererIsReusable checks unregistered metrics can be built twice, which a real registry refuses.
func TestNilRegistererIsReusable(t *testing.T) {
	NewMayfly(nil).RegisterPool(func() []PoolRepo { return nil })
	NewMayfly(nil)
	NewReaper(nil)
	NewReaper(nil)
}
