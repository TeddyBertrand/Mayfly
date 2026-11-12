package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// ProvisionerConfig holds the settings of the provisioner component.
type ProvisionerConfig struct {
	AppID         int64
	AppKey        []byte
	GitHubAPIURL  string
	DockerSocket  string
	WorkerImage   string
	WorkerTimeout time.Duration
	Workers       int
	QueueSize     int
}

// LoadProvisioner reads the provisioner settings through getenv and readFile, applying defaults
// and returning an error on missing or invalid values. AppKey is PEM bytes and must not be logged.
func LoadProvisioner(getenv func(string) string, readFile func(string) ([]byte, error)) (ProvisionerConfig, error) {
	cfg := ProvisionerConfig{
		GitHubAPIURL: github.DefaultBaseURL,
		DockerSocket: docker.DefaultSocket,
		WorkerImage:  docker.DefaultImage,
	}

	v := getenv("MAYFLY_APP_ID")
	if v == "" {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_APP_ID: required")
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_APP_ID: must be a positive integer, got %q", v)
	}
	cfg.AppID = id

	if cfg.AppKey, err = loadSecretFile(getenv, readFile, "MAYFLY_APP_KEY_FILE"); err != nil {
		return ProvisionerConfig{}, err
	}

	if v := getenv("MAYFLY_GITHUB_API_URL"); v != "" {
		cfg.GitHubAPIURL = v
	}
	if v := getenv("MAYFLY_DOCKER_SOCKET"); v != "" {
		cfg.DockerSocket = v
	}
	if v := getenv("MAYFLY_WORKER_IMAGE"); v != "" {
		cfg.WorkerImage = v
	}
	if cfg.WorkerTimeout, err = parseDuration(getenv, "MAYFLY_WORKER_TIMEOUT", docker.DefaultTimeout); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.Workers, err = parsePositiveInt(getenv, "MAYFLY_WORKERS", 2); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.QueueSize, err = parsePositiveInt(getenv, "MAYFLY_QUEUE_SIZE", 100); err != nil {
		return ProvisionerConfig{}, err
	}
	return cfg, nil
}

// parseDuration reads a positive duration from env, or returns def when it is unset.
func parseDuration(getenv func(string) string, env string, def time.Duration) (time.Duration, error) {
	v := getenv(env)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", env, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %s", env, d)
	}
	return d, nil
}

// parsePositiveInt reads a positive integer from env, or returns def when it is unset.
func parsePositiveInt(getenv func(string) string, env string, def int) (int, error) {
	v := getenv(env)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s: must be a positive integer, got %q", env, v)
	}
	return n, nil
}
