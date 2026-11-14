package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthzReportsInstance checks /healthz answers 200 with JSON naming this process's instance ID.
func TestHealthzReportsInstance(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler("abc123").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

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

// TestRoutes checks the ops listener serves only GET /healthz.
func TestRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"healthz", http.MethodGet, "/healthz", http.StatusOK},
		{"healthz wrong method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{"no webhook here", http.MethodPost, "/webhook", http.StatusNotFound},
	}
	h := NewHandler("abc123")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
