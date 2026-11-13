package receiver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthzReportsInstance checks /healthz answers 200 with JSON naming this process's instance ID.
func TestHealthzReportsInstance(t *testing.T) {
	wh := testWebhook([]byte("s"), nil)
	wh.Instance = "abc123"
	rec := httptest.NewRecorder()
	NewHandler(discardLogger(), wh).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, content-type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var h Health
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if h != (Health{Status: "ok", Instance: "abc123"}) {
		t.Errorf("health = %+v", h)
	}
}
