package github

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

// RunStatusCompleted is the status of a workflow run or job that has finished, whatever its conclusion.
const RunStatusCompleted = "completed"

// workflowFilePattern accepts a workflow file name such as go-build.yml, so it cannot rewrite the request path.
var workflowFilePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.ya?ml$`)

// TokenClient calls the GitHub REST API with a fixed token, such as a user's `gh auth token`.
// It shares Client's request, error and rate-limit handling.
type TokenClient struct {
	c     *Client
	token string
}

// NewTokenClient returns a TokenClient that sends token to baseURL.
func NewTokenClient(baseURL, token string) *TokenClient {
	return &TokenClient{c: &Client{baseURL: baseURL, http: &http.Client{Timeout: requestTimeout}, now: time.Now}, token: token}
}

// WorkflowRun is the state of one workflow run.
type WorkflowRun struct {
	ID         int64
	Status     string
	Conclusion string
}

// Job is one job of a workflow run, with GitHub's timestamps.
type Job struct {
	ID          int64
	Name        string
	Status      string
	Conclusion  string
	RunnerName  string
	Labels      []string
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	Steps       []Step
}

// Step is one step of a job, in the order GitHub numbers them.
type Step struct {
	Number      int
	Name        string
	StartedAt   time.Time
	CompletedAt time.Time
}

// DispatchWorkflow starts the workflow file on ref with inputs and returns the new run's ID.
func (t *TokenClient) DispatchWorkflow(ctx context.Context, repo, file, ref string, inputs map[string]string) (int64, error) {
	if !repoPattern.MatchString(repo) {
		return 0, fmt.Errorf("dispatch workflow: invalid repo %q", repo)
	}
	if !workflowFilePattern.MatchString(file) {
		return 0, fmt.Errorf("dispatch workflow: invalid workflow file %q", file)
	}
	body := struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs,omitempty"`
		// Without it GitHub answers 204 and the run has to be found by searching.
		ReturnRunDetails bool `json:"return_run_details"`
	}{ref, inputs, true}
	var resp struct {
		RunID int64 `json:"workflow_run_id"`
	}
	path := fmt.Sprintf("/repos/%s/actions/workflows/%s/dispatches", repo, file)
	if err := t.c.do(ctx, http.MethodPost, path, t.token, body, &resp, http.StatusOK); err != nil {
		return 0, err
	}
	if resp.RunID == 0 {
		return 0, fmt.Errorf("github POST %s: no workflow_run_id in response", path)
	}
	return resp.RunID, nil
}

// Run returns the status and conclusion of workflow run runID.
func (t *TokenClient) Run(ctx context.Context, repo string, runID int64) (WorkflowRun, error) {
	if !repoPattern.MatchString(repo) {
		return WorkflowRun{}, fmt.Errorf("get run: invalid repo %q", repo)
	}
	var r struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	if err := t.c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/actions/runs/%d", repo, runID), t.token, nil, &r, http.StatusOK); err != nil {
		return WorkflowRun{}, err
	}
	return WorkflowRun(r), nil
}

// CancelRun asks GitHub to cancel workflow run runID. GitHub answers 409 for a run it cannot cancel,
// such as one stuck before its jobs were created.
func (t *TokenClient) CancelRun(ctx context.Context, repo string, runID int64) error {
	if !repoPattern.MatchString(repo) {
		return fmt.Errorf("cancel run: invalid repo %q", repo)
	}
	return t.c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/actions/runs/%d/cancel", repo, runID), t.token, nil, nil, http.StatusAccepted)
}

// RunJobs returns every job of the latest attempt of workflow run runID, with its steps.
func (t *TokenClient) RunJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("list run jobs: invalid repo %q", repo)
	}
	type step struct {
		Number      int       `json:"number"`
		Name        string    `json:"name"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
	}
	type job struct {
		ID          int64     `json:"id"`
		Name        string    `json:"name"`
		Status      string    `json:"status"`
		Conclusion  string    `json:"conclusion"`
		RunnerName  string    `json:"runner_name"`
		Labels      []string  `json:"labels"`
		CreatedAt   time.Time `json:"created_at"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
		Steps       []step    `json:"steps"`
	}
	type page struct {
		Jobs []job `json:"jobs"`
	}
	return list(ctx, t.c, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs", repo, runID), t.token, func(p page) []Job {
		out := make([]Job, 0, len(p.Jobs))
		for _, j := range p.Jobs {
			steps := make([]Step, 0, len(j.Steps))
			for _, s := range j.Steps {
				steps = append(steps, Step(s))
			}
			out = append(out, Job{
				ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion, RunnerName: j.RunnerName, Labels: j.Labels,
				CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt, Steps: steps,
			})
		}
		return out
	})
}
