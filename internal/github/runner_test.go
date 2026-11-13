package github

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestRunner checks found, not found and error answers from the runner lookup.
func TestRunner(t *testing.T) {
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runners/77", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":77,"status":"offline","busy":false}`))
	})
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runners/78", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runners/79", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, _ := newTestClient(t, f, time.Now())
	ctx := context.Background()

	if got, err := c.Runner(ctx, 9, "octo/demo", 77); err != nil || got != (Runner{Found: true, Status: "offline"}) {
		t.Errorf("runner 77 = %+v, %v", got, err)
	}
	if got, err := c.Runner(ctx, 9, "octo/demo", 78); err != nil || got.Found {
		t.Errorf("runner 78 = %+v, %v, want not found and no error", got, err)
	}
	if _, err := c.Runner(ctx, 9, "octo/demo", 79); err == nil {
		t.Error("runner 79: want an error on 500")
	}
	if _, err := c.Runner(ctx, 9, "../evil", 77); err == nil {
		t.Error("invalid repo accepted")
	}
}
