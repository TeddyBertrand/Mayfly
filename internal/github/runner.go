package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// The statuses GitHub reports for a self-hosted runner registration.
const (
	RunnerStatusOnline  = "online"
	RunnerStatusOffline = "offline"
)

// Runner is a self-hosted runner registration as GitHub reports it.
type Runner struct {
	// Found is false when GitHub has no registration with that ID (404).
	Found bool
	// Status is RunnerStatusOnline or RunnerStatusOffline.
	Status string
	Busy   bool
}

// Runner looks up runner runnerID on repo. A missing registration is reported as Found false, not as an error.
func (c *Client) Runner(ctx context.Context, installationID int64, repo string, runnerID int64) (Runner, error) {
	if !repoPattern.MatchString(repo) {
		return Runner{}, fmt.Errorf("get runner: invalid repo %q", repo)
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return Runner{}, err
	}
	var resp struct {
		Status string `json:"status"`
		Busy   bool   `json:"busy"`
	}
	path := fmt.Sprintf("/repos/%s/actions/runners/%d", repo, runnerID)
	err = c.do(ctx, http.MethodGet, path, token, nil, &resp, http.StatusOK)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return Runner{Found: false}, nil
	}
	if err != nil {
		return Runner{}, err
	}
	return Runner{Found: true, Status: resp.Status, Busy: resp.Busy}, nil
}
