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
	// A vector only shows up once a label value has been used.
	m.Jobs.WithLabelValues("success").Inc()

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
	NewMayfly(nil)
	NewMayfly(nil)
	NewReaper(nil)
	NewReaper(nil)
}
