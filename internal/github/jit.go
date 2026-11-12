package github

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
)

// repoPattern accepts "owner/name" with GitHub's allowed characters, so a payload value
// cannot rewrite the request path.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// defaultRunnerGroupID is the "Default" runner group every repository has.
const defaultRunnerGroupID = 1

// JITConfig is a one-shot runner registration. Encoded is a credential and must not be logged.
type JITConfig struct {
	RunnerID int64
	Encoded  string
}

// String hides the encoded config so it cannot leak through fmt.
func (j JITConfig) String() string {
	return fmt.Sprintf("JITConfig{RunnerID:%d Encoded:[redacted]}", j.RunnerID)
}

// LogValue hides the encoded config so it cannot leak through slog.
func (j JITConfig) LogValue() slog.Value {
	return slog.GroupValue(slog.Int64("runner_id", j.RunnerID), slog.String("encoded", "[redacted]"))
}

// RunnerName returns a unique runner name that embeds jobID, e.g. "mayfly-123-a3f9c1".
func RunnerName(jobID int64) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("runner name: %w", err)
	}
	return fmt.Sprintf("mayfly-%d-%s", jobID, hex.EncodeToString(b)), nil
}

// GenerateJITConfig registers a just-in-time runner called name with labels on repo ("owner/name").
func (c *Client) GenerateJITConfig(ctx context.Context, installationID int64, repo, name string, labels []string) (JITConfig, error) {
	if !repoPattern.MatchString(repo) {
		return JITConfig{}, fmt.Errorf("generate jit config: invalid repo %q", repo)
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return JITConfig{}, err
	}

	req := struct {
		Name          string   `json:"name"`
		RunnerGroupID int64    `json:"runner_group_id"`
		Labels        []string `json:"labels"`
		WorkFolder    string   `json:"work_folder"`
	}{name, defaultRunnerGroupID, labels, "_work"}
	var resp struct {
		Runner struct {
			ID int64 `json:"id"`
		} `json:"runner"`
		EncodedJITConfig string `json:"encoded_jit_config"`
	}

	path := "/repos/" + repo + "/actions/runners/generate-jitconfig"
	if err := c.do(ctx, http.MethodPost, path, token, req, &resp, http.StatusCreated); err != nil {
		return JITConfig{}, err
	}
	if resp.EncodedJITConfig == "" {
		return JITConfig{}, fmt.Errorf("github POST %s: empty jit config in response", path)
	}
	return JITConfig{RunnerID: resp.Runner.ID, Encoded: resp.EncodedJITConfig}, nil
}

// DeleteRunner removes the self-hosted runner registration runnerID from repo.
func (c *Client) DeleteRunner(ctx context.Context, installationID int64, repo string, runnerID int64) error {
	if !repoPattern.MatchString(repo) {
		return fmt.Errorf("delete runner: invalid repo %q", repo)
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/actions/runners/%d", repo, runnerID)
	return c.do(ctx, http.MethodDelete, path, token, nil, nil, http.StatusNoContent)
}
