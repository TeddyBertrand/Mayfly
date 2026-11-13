package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// pageOf returns the slice of items for the page and per_page query values of r.
func pageOf[T any](t *testing.T, r *http.Request, items []T) []T {
	t.Helper()
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 || size != perPage {
		t.Errorf("bad paging query %q", r.URL.RawQuery)
		return nil
	}
	start := min((page-1)*size, len(items))
	return items[start:min(start+size, len(items))]
}

// TestRepos checks installations and their repositories are combined, each tagged with its installation.
func TestRepos(t *testing.T) {
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	f.mux.HandleFunc("GET /app/installations", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":9},{"id":10}]`))
	})
	f.mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{"Bearer tok-9-1": "octo/demo", "Bearer tok-10-2": "octo/other"}[r.Header.Get("Authorization")]
		_, _ = fmt.Fprintf(w, `{"total_count":1,"repositories":[{"full_name":%q}]}`, name)
	})
	c, _ := newTestClient(t, f, time.Now())

	got, err := c.Repos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Repo{{FullName: "octo/demo", InstallationID: 9}, {FullName: "octo/other", InstallationID: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %+v, want %+v", got, want)
	}
}

// TestRunnersPaginates checks every page is fetched until a short one, here 150 runners over two pages.
func TestRunnersPaginates(t *testing.T) {
	type runner struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Busy   bool   `json:"busy"`
	}
	all := make([]runner, 150)
	for i := range all {
		all[i] = runner{ID: int64(i + 1), Name: fmt.Sprintf("mayfly-%d-aa", i), Status: "offline"}
	}
	calls := 0
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runners", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(all), "runners": pageOf(t, r, all)})
	})
	c, _ := newTestClient(t, f, time.Now())

	got, err := c.Runners(context.Background(), 9, "octo/demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 || calls != 2 || got[149] != (RunnerRegistration{ID: 150, Name: "mayfly-149-aa", Status: "offline"}) {
		t.Errorf("got %d runners in %d calls, last %+v", len(got), calls, got[len(got)-1])
	}
	if _, err := c.Runners(context.Background(), 9, "../x"); err == nil {
		t.Error("invalid repo accepted")
	}
}

// TestQueuedJobs checks only jobs still in queued are returned, across every queued run.
func TestQueuedJobs(t *testing.T) {
	created := time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC)
	f := newFakeGitHub(t, time.Now().Add(time.Hour))
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "queued" {
			t.Errorf("runs query = %q, want status=queued", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1},{"id":2}]}`))
	})
	f.mux.HandleFunc("GET /repos/octo/demo/actions/runs/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "1":
			_, _ = fmt.Fprintf(w, `{"jobs":[
				{"id":11,"run_id":1,"name":"build","status":"queued","labels":["self-hosted","mayfly"],"created_at":%q,"html_url":"https://gh/j/11"},
				{"id":12,"run_id":1,"name":"lint","status":"in_progress","labels":["mayfly"]}]}`, created.Format(time.RFC3339))
		case "2":
			_, _ = w.Write([]byte(`{"jobs":[{"id":21,"run_id":2,"name":"done","status":"completed","labels":["mayfly"]}]}`))
		}
	})
	c, _ := newTestClient(t, f, time.Now())

	got, err := c.QueuedJobs(context.Background(), 9, "octo/demo")
	if err != nil {
		t.Fatal(err)
	}
	want := []QueuedJob{{ID: 11, RunID: 1, Name: "build", Labels: []string{"self-hosted", "mayfly"}, CreatedAt: created, URL: "https://gh/j/11"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("jobs = %+v, want %+v", got, want)
	}
}
