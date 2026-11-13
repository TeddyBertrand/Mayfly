package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/retry"
)

// TestAPIErrorRetryable checks which GitHub answers are retried and how long each asks us to wait.
func TestAPIErrorRetryable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name     string
		status   int
		headers  map[string]string
		wantTemp bool
		wantWait time.Duration
	}{
		{"server error", http.StatusBadGateway, nil, true, 0},
		{"too many requests with retry-after", http.StatusTooManyRequests, map[string]string{"Retry-After": "7"}, true, 7 * time.Second},
		{"secondary rate limit", http.StatusForbidden, map[string]string{"Retry-After": "60"}, true, time.Minute},
		{"primary rate limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now.Unix()+12, 10)}, true, 12 * time.Second},
		{"forbidden, not a rate limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "4999"}, false, 0},
		{"not found", http.StatusNotFound, nil, false, 0},
		{"unprocessable", http.StatusUnprocessableEntity, nil, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, 1, testKey(t))
			c.now = func() time.Time { return now }
			_, err := c.InstallationToken(context.Background(), 1)

			if got := retry.Temporary(err); got != tt.wantTemp {
				t.Errorf("Temporary = %v, want %v (err %v)", got, tt.wantTemp, err)
			}
			if got := retry.After(err); got != tt.wantWait {
				t.Errorf("After = %s, want %s", got, tt.wantWait)
			}
		})
	}
}

// TestNetworkErrorRetryable checks that a request with no answer at all is treated as temporary.
func TestNetworkErrorRetryable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	_, err := NewClient(srv.URL, 1, testKey(t)).InstallationToken(context.Background(), 1)
	var apiErr *APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a transport error", err)
	}
	if !retry.Temporary(err) {
		t.Errorf("Temporary = false, want true for %v", err)
	}
}
