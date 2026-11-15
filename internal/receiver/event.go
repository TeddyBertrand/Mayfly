package receiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// workflowJobEvent mirrors the fields Mayfly reads from a workflow_job payload.
type workflowJobEvent struct {
	Action      workflow.Action `json:"action"`
	WorkflowJob struct {
		ID         int64    `json:"id"`
		RunID      int64    `json:"run_id"`
		Name       string   `json:"name"`
		Labels     []string `json:"labels"`
		RunnerName string   `json:"runner_name"`
		Conclusion string   `json:"conclusion"`
		// GitHub sends null for a timestamp not reached yet, which leaves it zero.
		CreatedAt   time.Time `json:"created_at"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// parseWorkflowJob decodes a workflow_job payload into its action and Job.
func parseWorkflowJob(body []byte) (workflow.Action, workflow.Job, error) {
	var ev workflowJobEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", workflow.Job{}, fmt.Errorf("decode workflow_job: %w", err)
	}
	if ev.Action == "" || ev.WorkflowJob.ID == 0 {
		return "", workflow.Job{}, errors.New("decode workflow_job: missing action or job id")
	}
	return ev.Action, workflow.Job{
		ID:             ev.WorkflowJob.ID,
		RunID:          ev.WorkflowJob.RunID,
		Name:           ev.WorkflowJob.Name,
		Repo:           ev.Repository.FullName,
		InstallationID: ev.Installation.ID,
		Labels:         ev.WorkflowJob.Labels,
		RunnerName:     ev.WorkflowJob.RunnerName,
		Conclusion:     ev.WorkflowJob.Conclusion,
		CreatedAt:      ev.WorkflowJob.CreatedAt,
		StartedAt:      ev.WorkflowJob.StartedAt,
		CompletedAt:    ev.WorkflowJob.CompletedAt,
	}, nil
}
