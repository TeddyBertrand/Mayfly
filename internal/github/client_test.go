package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub is an httptest server that mints installation tokens and counts the requests.
type fakeGitHub struct {
	*httptest.Server
	tokenCalls atomic.Int32
	expiresAt  time.Time
	mux        *http.ServeMux
}

// newFakeGitHub starts a fake GitHub that accepts JWTs signed by testKey and issues tokens expiring at expiresAt.
func newFakeGitHub(t *testing.T, expiresAt time.Time) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{expiresAt: expiresAt, mux: http.NewServeMux()}
	f.mux.HandleFunc("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		jwt, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		verifyJWT(t, jwt, &testKey(t).PublicKey)
		n := f.tokenCalls.Add(1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      fmt.Sprintf("tok-%s-%d", r.PathValue("id"), n),
			"expires_at": f.expiresAt.Format(time.RFC3339),
		})
	})
	f.Server = httptest.NewServer(f.mux)
	t.Cleanup(f.Close)
	return f
}

// newTestClient returns a Client pointed at f with a manual clock starting at start.
func newTestClient(t *testing.T, f *fakeGitHub, start time.Time) (*Client, func(time.Duration)) {
	t.Helper()
	now := start
	c := NewClient(f.URL, 5714458, testKey(t))
	c.now = func() time.Time { return now }
	return c, func(d time.Duration) { now = now.Add(d) }
}

// TestInstallationTokenCaching checks reuse, per-installation entries and refresh near expiry.
func TestInstallationTokenCaching(t *testing.T) {
	start := time.Now().Truncate(time.Second)
	f := newFakeGitHub(t, start.Add(time.Hour))
	c, advance := newTestClient(t, f, start)
	ctx := context.Background()

	first, err := c.InstallationToken(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := c.InstallationToken(ctx, 1)
	if again != first || f.tokenCalls.Load() != 1 {
		t.Fatalf("cached token not reused: calls = %d", f.tokenCalls.Load())
	}

	if _, err := c.InstallationToken(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if f.tokenCalls.Load() != 2 {
		t.Fatalf("second installation did not get its own token: calls = %d", f.tokenCalls.Load())
	}

	advance(54 * time.Minute)
	if tok, _ := c.InstallationToken(ctx, 1); tok != first {
		t.Fatal("token refreshed while more than 5 minutes remained")
	}

	f.expiresAt = start.Add(2 * time.Hour)
	advance(2 * time.Minute)
	refreshed, err := c.InstallationToken(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed == first || f.tokenCalls.Load() != 3 {
		t.Fatalf("token not refreshed within 5 minutes of expiry: calls = %d", f.tokenCalls.Load())
	}
}

// TestInstallationTokenAPIError checks that a GitHub error becomes an APIError with status and message.
func TestInstallationTokenAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"A JSON web token could not be decoded"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 1, testKey(t))
	_, err := c.InstallationToken(context.Background(), 1)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized || apiErr.Message == "" {
		t.Errorf("got %+v", apiErr)
	}
}
