package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// TestInventoryList checks the label filter sent to the daemon and that labels are parsed back into a Worker.
func TestInventoryList(t *testing.T) {
	var gotFilters string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+apiVersion+"/containers/json" || r.URL.Query().Get("all") != "true" {
			t.Errorf("unexpected request %s", r.URL)
		}
		gotFilters = r.URL.Query().Get("filters")
		_, _ = w.Write([]byte(`[
			{"Id":"c1","Created":1700000000,"Labels":{"mayfly.job_id":"42","mayfly.runner_name":"mayfly-42-ab","mayfly.owner":"inst-a","mayfly.repo":"octo/demo","mayfly.installation_id":"9","mayfly.runner_id":"77"}},
			{"Id":"c2","Created":1700000100,"Labels":{"mayfly.job_id":"43"}}
		]`))
	}))
	defer srv.Close()

	inv := &Inventory{eng: &engine{http: srv.Client(), base: srv.URL + "/" + apiVersion}}
	got, err := inv.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var filters map[string][]string
	if err := json.Unmarshal([]byte(gotFilters), &filters); err != nil || !reflect.DeepEqual(filters["label"], []string{LabelJobID}) {
		t.Errorf("filters = %q", gotFilters)
	}
	want := []Worker{
		{ID: "c1", Created: time.Unix(1700000000, 0), JobID: 42, RunnerName: "mayfly-42-ab", Owner: "inst-a", Repo: "octo/demo", InstallationID: 9, RunnerID: 77},
		{ID: "c2", Created: time.Unix(1700000100, 0), JobID: 43},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("workers =\n%+v\nwant\n%+v", got, want)
	}
}
