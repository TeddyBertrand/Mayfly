package config

import (
	"fmt"
	"strconv"
	"time"
)

// Defaults for settings shared by several processes. config owns them so it imports no other internal package.
const (
	// DefaultGitHubAPIURL is the public GitHub REST API.
	DefaultGitHubAPIURL = "https://api.github.com"
	// DefaultDockerSocket is where the Docker daemon listens on Linux.
	DefaultDockerSocket = "/var/run/docker.sock"
	// DefaultWorkerImage is the official runner image, pinned to an exact release.
	DefaultWorkerImage = "ghcr.io/actions/actions-runner:2.338.2"
	// DefaultWorkerTimeout matches GitHub's maximum job duration.
	DefaultWorkerTimeout = 6 * time.Hour
	// DefaultOpsAddr keeps the ops endpoints on loopback unless a deploy says otherwise (decision 018).
	DefaultOpsAddr = "127.0.0.1:8081"
	// DefaultHealthURL is where the reaper finds mayfly's /healthz when both run on one host.
	DefaultHealthURL = "http://" + DefaultOpsAddr + "/healthz"
)

// LoadOpsAddr returns the address of the ops listener, which serves /healthz and must never be published.
func LoadOpsAddr(getenv func(string) string) string {
	if v := getenv("MAYFLY_OPS_ADDR"); v != "" {
		return v
	}
	return DefaultOpsAddr
}

// GitHubApp holds the GitHub App credentials and API location. Key is PEM bytes and must not be logged.
type GitHubApp struct {
	ID     int64
	Key    []byte
	APIURL string
}

// Docker holds the Docker settings shared by the processes that talk to the daemon.
type Docker struct {
	Socket string
}

// LoadGitHubApp reads the GitHub App settings through getenv and readFile, returning an error on missing or invalid values.
func LoadGitHubApp(getenv func(string) string, readFile func(string) ([]byte, error)) (GitHubApp, error) {
	app := GitHubApp{APIURL: DefaultGitHubAPIURL}

	v := getenv("MAYFLY_APP_ID")
	if v == "" {
		return GitHubApp{}, fmt.Errorf("MAYFLY_APP_ID: required")
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return GitHubApp{}, fmt.Errorf("MAYFLY_APP_ID: must be a positive integer, got %q", v)
	}
	app.ID = id

	if app.Key, err = loadSecretFile(getenv, readFile, "MAYFLY_APP_KEY_FILE"); err != nil {
		return GitHubApp{}, err
	}
	if v := getenv("MAYFLY_GITHUB_API_URL"); v != "" {
		app.APIURL = v
	}
	return app, nil
}

// loadDocker reads the shared Docker settings through getenv.
func loadDocker(getenv func(string) string) Docker {
	d := Docker{Socket: DefaultDockerSocket}
	if v := getenv("MAYFLY_DOCKER_SOCKET"); v != "" {
		d.Socket = v
	}
	return d
}

// loadWorkerTimeout reads how long a worker may run, which the provisioner enforces and the reaper must outlast.
func loadWorkerTimeout(getenv func(string) string) (time.Duration, error) {
	return parseDuration(getenv, "MAYFLY_WORKER_TIMEOUT", DefaultWorkerTimeout)
}
