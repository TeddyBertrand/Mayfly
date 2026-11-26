package github

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/TeddyBertrand/Mayfly/internal/logkey"
)

// repoPattern accepts "owner/name" with GitHub's allowed characters, so a payload value
// cannot rewrite the request path.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// RunnerNamePrefix starts every runner name Mayfly registers; the reaper only touches runners named this way.
const RunnerNamePrefix = "mayfly-"

const (
	// defaultRunnerGroupID is the "Default" runner group every repository has.
	defaultRunnerGroupID = 1
	// runnerSuffixBytes is how many random bytes make a runner name unique; hex doubles the length.
	runnerSuffixBytes = 3
	// workFolder is the runner's work directory, relative to its install directory.
	workFolder = "_work"
	// redacted replaces a credential in logs.
	redacted = "[redacted]"
)

// JITConfig is a one-shot runner registration. Encoded is a credential and must not be logged.
type JITConfig struct {
	RunnerID int64
	Encoded  string
}

// String hides the encoded config so it cannot leak through fmt.
func (j JITConfig) String() string {
	return fmt.Sprintf("JITConfig{RunnerID:%d Encoded:%s}", j.RunnerID, redacted)
}

// LogValue hides the encoded config so it cannot leak through slog.
func (j JITConfig) LogValue() slog.Value {
	return slog.GroupValue(slog.Int64(logkey.RunnerID, j.RunnerID), slog.String(logkey.Encoded, redacted))
}

// warmRunnerTag marks a warm pool runner's name in place of a job ID, since no job exists yet.
const warmRunnerTag = "warm"

// RunnerName returns a unique runner name that embeds jobID, e.g. "mayfly-123-a3f9c1".
func RunnerName(jobID int64) (string, error) {
	return runnerName(strconv.FormatInt(jobID, 10))
}

// WarmRunnerName returns a unique name for a warm pool runner, e.g. "mayfly-warm-a3f9c1".
func WarmRunnerName() (string, error) {
	return runnerName(warmRunnerTag)
}

// runnerName returns RunnerNamePrefix, then tag, then a random hex suffix.
func runnerName(tag string) (string, error) {
	b := make([]byte, runnerSuffixBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("runner name: %w", err)
	}
	return fmt.Sprintf("%s%s-%s", RunnerNamePrefix, tag, hex.EncodeToString(b)), nil
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
	}{name, defaultRunnerGroupID, labels, workFolder}
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
