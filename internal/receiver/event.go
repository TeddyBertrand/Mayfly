package receiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Job is the part of a workflow_job event the provisioner needs to start a worker.
type Job struct {
	ID             int64
	RunID          int64
	Name           string
	Repo           string
	InstallationID int64
	Labels         []string
	// RunnerName is set once a runner picks the job up (in_progress, completed).
	RunnerName string
	// Conclusion is set on completed, e.g. "success" or "cancelled".
	Conclusion string
}

// workflowJobEvent mirrors the fields Mayfly reads from a workflow_job payload.
type workflowJobEvent struct {
	Action      string `json:"action"`
	WorkflowJob struct {
		ID         int64    `json:"id"`
		RunID      int64    `json:"run_id"`
		Name       string   `json:"name"`
		Labels     []string `json:"labels"`
		RunnerName string   `json:"runner_name"`
		Conclusion string   `json:"conclusion"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// parseWorkflowJob decodes a workflow_job payload into its action and Job.
func parseWorkflowJob(body []byte) (string, Job, error) {
	var ev workflowJobEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", Job{}, fmt.Errorf("decode workflow_job: %w", err)
	}
	if ev.Action == "" || ev.WorkflowJob.ID == 0 {
		return "", Job{}, errors.New("decode workflow_job: missing action or job id")
	}
	return ev.Action, Job{
		ID:             ev.WorkflowJob.ID,
		RunID:          ev.WorkflowJob.RunID,
		Name:           ev.WorkflowJob.Name,
		Repo:           ev.Repository.FullName,
		InstallationID: ev.Installation.ID,
		Labels:         ev.WorkflowJob.Labels,
		RunnerName:     ev.WorkflowJob.RunnerName,
		Conclusion:     ev.WorkflowJob.Conclusion,
	}, nil
}

// MatchesLabels reports whether the job requests every label in required, ignoring case as GitHub does.
func (j Job) MatchesLabels(required []string) bool {
	for _, want := range required {
		found := false
		for _, l := range j.Labels {
			if strings.EqualFold(l, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
