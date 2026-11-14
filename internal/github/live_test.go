package github

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// envOr returns the environment value for key, or def when it is unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// logInstallationAccess logs the installation's granted permissions and reachable repos, to diagnose 403s.
func logInstallationAccess(ctx context.Context, t *testing.T, c *Client, installationID int64) {
	t.Helper()
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		t.Fatal(err)
	}
	var inst struct {
		RepositorySelection string            `json:"repository_selection"`
		Permissions         map[string]string `json:"permissions"`
	}
	if err := c.do(ctx, "GET", fmt.Sprintf("/app/installations/%d", installationID), jwt, nil, &inst, 200); err != nil {
		t.Fatalf("get installation: %v", err)
	}
	t.Logf("installation permissions: %v (repository_selection: %s)", inst.Permissions, inst.RepositorySelection)

	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		t.Fatal(err)
	}
	var repos struct {
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := c.do(ctx, "GET", "/installation/repositories", token, nil, &repos, 200); err != nil {
		t.Fatalf("list installation repos: %v", err)
	}
	names := make([]string, 0, len(repos.Repositories))
	for _, r := range repos.Repositories {
		names = append(names, r.FullName)
	}
	t.Logf("installation repos: %v", names)
}

// TestLiveGenerateJITConfig calls the real GitHub API. It runs only with MAYFLY_LIVE_TEST=1
// and MAYFLY_APP_KEY_FILE set, and deletes the runner registration it creates.
func TestLiveGenerateJITConfig(t *testing.T) {
	if os.Getenv("MAYFLY_LIVE_TEST") != "1" {
		t.Skip("set MAYFLY_LIVE_TEST=1 to call the real GitHub API")
	}
	keyFile := os.Getenv("MAYFLY_APP_KEY_FILE")
	if keyFile == "" {
		t.Fatal("MAYFLY_APP_KEY_FILE is required")
	}
	appID, err := strconv.ParseInt(envOr("MAYFLY_APP_ID", "5714458"), 10, 64)
	if err != nil {
		t.Fatalf("MAYFLY_APP_ID: %v", err)
	}
	installationID, err := strconv.ParseInt(envOr("MAYFLY_INSTALLATION_ID", "180929139"), 10, 64)
	if err != nil {
		t.Fatalf("MAYFLY_INSTALLATION_ID: %v", err)
	}
	repo := envOr("MAYFLY_LIVE_REPO", "TeddyBertrand/mayfly-demo")

	pemBytes, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	key, err := ParsePrivateKey(pemBytes)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := NewClient(config.DefaultGitHubAPIURL, appID, key)

	logInstallationAccess(ctx, t, c, installationID)

	name, err := RunnerName(time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := c.GenerateJITConfig(ctx, installationID, repo, name, []string{"mayfly-live-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.DeleteRunner(context.Background(), installationID, repo, cfg.RunnerID); err != nil {
			t.Errorf("cleanup: delete runner %d: %v", cfg.RunnerID, err)
		}
	})

	if cfg.RunnerID == 0 || len(cfg.Encoded) < 100 {
		t.Fatalf("unexpected jit config: runner_id=%d encoded_len=%d", cfg.RunnerID, len(cfg.Encoded))
	}
	t.Logf("created runner %q (id %d), encoded config %d bytes", name, cfg.RunnerID, len(cfg.Encoded))
}
