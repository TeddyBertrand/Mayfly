package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// fakeActionsAPI serves the dispatch, run and jobs endpoints for one run, checking the bearer token.
func fakeActionsAPI(t *testing.T, dispatchStatus int) (*httptest.Server, *map[string]any) {
	t.Helper()
	var dispatched map[string]any
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /repos/o/r/actions/workflows/go-build.yml/dispatches", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&dispatched)
		w.WriteHeader(dispatchStatus)
		if dispatchStatus == http.StatusOK {
			_, _ = w.Write([]byte(`{"workflow_run_id": 42, "run_url": "x", "html_url": "y"}`))
		}
	})
	mux.HandleFunc("GET /repos/o/r/actions/runs/42", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`{"id": 42, "status": "completed", "conclusion": "success"}`))
		}
	})
	mux.HandleFunc("POST /repos/o/r/actions/runs/42/cancel", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.WriteHeader(http.StatusAccepted)
		}
	})
	mux.HandleFunc("POST /repos/o/r/actions/runs/43/cancel", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Cannot cancel a workflow run that is completed."}`))
		}
	})
	mux.HandleFunc("GET /repos/o/r/actions/runs/42/jobs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"total_count": 1, "jobs": [{
			"id": 7, "name": "build", "status": "completed", "conclusion": "success",
			"runner_name": "mayfly-7-abc", "labels": ["self-hosted", "mayfly"],
			"created_at": "2026-11-16T15:21:17Z", "started_at": "2026-11-16T15:21:25Z", "completed_at": "2026-11-16T15:21:57Z",
			"steps": [
				{"number": 1, "name": "Set up job", "started_at": "2026-11-16T15:21:27Z", "completed_at": "2026-11-16T15:21:29Z"},
				{"number": 2, "name": "Run actions/checkout@v4", "started_at": "2026-11-16T15:21:29Z", "completed_at": "2026-11-16T15:21:30Z"}
			]}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &dispatched
}

// TestDispatchWorkflow checks the request body and that the run ID comes back.
func TestDispatchWorkflow(t *testing.T) {
	srv, dispatched := fakeActionsAPI(t, http.StatusOK)
	c := NewTokenClient(srv.URL, "user-token")

	id, err := c.DispatchWorkflow(context.Background(), "o/r", "go-build.yml", "main", map[string]string{"runner": "mayfly"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Errorf("run id = %d, want 42", id)
	}
	want := map[string]any{"ref": "main", "inputs": map[string]any{"runner": "mayfly"}, "return_run_details": true}
	if !reflect.DeepEqual(*dispatched, want) {
		t.Errorf("body = %v, want %v", *dispatched, want)
	}
}

// TestDispatchWorkflowErrors checks bad arguments, an API error and a response without a run ID.
func TestDispatchWorkflowErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		repo   string
		file   string
		token  string
	}{
		{"invalid repo", http.StatusOK, "o/r/../x", "go-build.yml", "user-token"},
		{"invalid file", http.StatusOK, "o/r", "../go-build.yml", "user-token"},
		{"wrong token", http.StatusOK, "o/r", "go-build.yml", "other"},
		{"204 without run details", http.StatusNoContent, "o/r", "go-build.yml", "user-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := fakeActionsAPI(t, tt.status)
			if _, err := NewTokenClient(srv.URL, tt.token).DispatchWorkflow(context.Background(), tt.repo, tt.file, "main", nil); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// TestDispatchWorkflowAPIError checks that a GitHub error keeps its status for the caller.
func TestDispatchWorkflowAPIError(t *testing.T) {
	srv, _ := fakeActionsAPI(t, http.StatusOK)
	_, err := NewTokenClient(srv.URL, "other").DispatchWorkflow(context.Background(), "o/r", "go-build.yml", "main", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Errorf("err = %v, want a 401 APIError", err)
	}
}

// TestRun checks that the run's status and conclusion are decoded.
func TestRun(t *testing.T) {
	srv, _ := fakeActionsAPI(t, http.StatusOK)
	got, err := NewTokenClient(srv.URL, "user-token").Run(context.Background(), "o/r", 42)
	if err != nil {
		t.Fatal(err)
	}
	if want := (WorkflowRun{ID: 42, Status: RunStatusCompleted, Conclusion: "success"}); got != want {
		t.Errorf("run = %+v, want %+v", got, want)
	}
}

// TestCancelRun checks a accepted cancel and a 409 refusal.
func TestCancelRun(t *testing.T) {
	srv, _ := fakeActionsAPI(t, http.StatusOK)
	c := NewTokenClient(srv.URL, "user-token")
	if err := c.CancelRun(context.Background(), "o/r", 42); err != nil {
		t.Errorf("cancel 42: %v", err)
	}
	var apiErr *APIError
	if err := c.CancelRun(context.Background(), "o/r", 43); !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Errorf("cancel 43: err = %v, want a 409 APIError", err)
	}
}

// TestRunJobs checks that job and step timestamps are decoded.
func TestRunJobs(t *testing.T) {
	srv, _ := fakeActionsAPI(t, http.StatusOK)
	jobs, err := NewTokenClient(srv.URL, "user-token").RunJobs(context.Background(), "o/r", 42)
	if err != nil {
		t.Fatal(err)
	}
	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	want := []Job{{
		ID: 7, Name: "build", Status: RunStatusCompleted, Conclusion: "success", RunnerName: "mayfly-7-abc",
		Labels:    []string{"self-hosted", "mayfly"},
		CreatedAt: ts("2026-11-16T15:21:17Z"), StartedAt: ts("2026-11-16T15:21:25Z"), CompletedAt: ts("2026-11-16T15:21:57Z"),
		Steps: []Step{
			{Number: 1, Name: "Set up job", StartedAt: ts("2026-11-16T15:21:27Z"), CompletedAt: ts("2026-11-16T15:21:29Z")},
			{Number: 2, Name: "Run actions/checkout@v4", StartedAt: ts("2026-11-16T15:21:29Z"), CompletedAt: ts("2026-11-16T15:21:30Z")},
		},
	}}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("jobs = %+v\nwant %+v", jobs, want)
	}
}
