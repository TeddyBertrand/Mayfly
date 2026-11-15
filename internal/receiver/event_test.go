package receiver

import (
	"reflect"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// queuedPayload is a trimmed workflow_job "queued" payload in GitHub's shape.
const queuedPayload = `{
  "action": "queued",
  "workflow_job": {"id": 42, "run_id": 7, "name": "build", "labels": ["self-hosted", "mayfly"], "status": "queued"},
  "repository": {"full_name": "octo/demo"},
  "installation": {"id": 99},
  "sender": {"login": "octocat"}
}`

// completedPayload returns a trimmed workflow_job "completed" payload with the given conclusion and runner.
func completedPayload(conclusion, runner string) string {
	return `{"action":"completed","workflow_job":{"id":42,"run_id":7,"name":"build","labels":["self-hosted","mayfly"],` +
		`"conclusion":"` + conclusion + `","runner_name":"` + runner + `"},"repository":{"full_name":"octo/demo"},"installation":{"id":99}}`
}

// TestParseWorkflowJob checks decoding of valid, partial and malformed payloads.
func TestParseWorkflowJob(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantAction workflow.Action
		wantJob    workflow.Job
		wantErr    bool
	}{
		{
			name:       "queued",
			body:       queuedPayload,
			wantAction: workflow.ActionQueued,
			wantJob:    workflow.Job{ID: 42, RunID: 7, Name: "build", Repo: "octo/demo", InstallationID: 99, Labels: []string{"self-hosted", "mayfly"}},
		},
		{
			name:       "completed cancelled",
			body:       completedPayload("cancelled", "mayfly-42-ab"),
			wantAction: workflow.ActionCompleted,
			wantJob: workflow.Job{ID: 42, RunID: 7, Name: "build", Repo: "octo/demo", InstallationID: 99, Labels: []string{"self-hosted", "mayfly"},
				RunnerName: "mayfly-42-ab", Conclusion: "cancelled"},
		},
		{
			name: "timestamps, null left zero",
			body: `{"action":"in_progress","workflow_job":{"id":42,"created_at":"2026-11-15T10:00:00Z",` +
				`"started_at":"2026-11-15T10:00:12Z","completed_at":null}}`,
			wantAction: workflow.ActionInProgress,
			wantJob: workflow.Job{ID: 42, CreatedAt: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
				StartedAt: time.Date(2026, 11, 15, 10, 0, 12, 0, time.UTC)},
		},
		{"not json", `{`, "", workflow.Job{}, true},
		{"missing job id", `{"action":"queued","workflow_job":{}}`, "", workflow.Job{}, true},
		{"missing action", `{"workflow_job":{"id":1}}`, "", workflow.Job{}, true},
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
