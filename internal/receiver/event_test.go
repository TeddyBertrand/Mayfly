package receiver

import (
	"reflect"
	"testing"
)

// queuedPayload is a trimmed workflow_job "queued" payload in GitHub's shape.
const queuedPayload = `{
  "action": "queued",
  "workflow_job": {"id": 42, "run_id": 7, "name": "build", "labels": ["self-hosted", "mayfly"], "status": "queued"},
  "repository": {"full_name": "octo/demo"},
  "installation": {"id": 99},
  "sender": {"login": "octocat"}
}`

// TestParseWorkflowJob checks decoding of valid, partial and malformed payloads.
func TestParseWorkflowJob(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantAction string
		wantJob    Job
		wantErr    bool
	}{
		{
			name:       "queued",
			body:       queuedPayload,
			wantAction: "queued",
			wantJob:    Job{ID: 42, RunID: 7, Name: "build", Repo: "octo/demo", InstallationID: 99, Labels: []string{"self-hosted", "mayfly"}},
		},
		{"not json", `{`, "", Job{}, true},
		{"missing job id", `{"action":"queued","workflow_job":{}}`, "", Job{}, true},
		{"missing action", `{"workflow_job":{"id":1}}`, "", Job{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, job, err := parseWorkflowJob([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if action != tt.wantAction || !reflect.DeepEqual(job, tt.wantJob) {
				t.Errorf("got (%q, %+v), want (%q, %+v)", action, job, tt.wantAction, tt.wantJob)
			}
		})
	}
}

// TestMatchesLabels checks that every required label must be present, case-insensitively.
func TestMatchesLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   []string
		required []string
		want     bool
	}{
		{"exact", []string{"mayfly"}, []string{"mayfly"}, true},
		{"with self-hosted", []string{"self-hosted", "mayfly"}, []string{"mayfly"}, true},
		{"case-insensitive", []string{"Mayfly"}, []string{"mayfly"}, true},
		{"github-hosted", []string{"ubuntu-latest"}, []string{"mayfly"}, false},
		{"missing one of two", []string{"mayfly"}, []string{"mayfly", "linux"}, false},
		{"no labels", nil, []string{"mayfly"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Job{Labels: tt.labels}).MatchesLabels(tt.required); got != tt.want {
				t.Errorf("MatchesLabels = %v, want %v", got, tt.want)
			}
		})
	}
}
