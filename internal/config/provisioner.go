package config

import (
	"fmt"
	"strconv"
	"time"
)

// ProvisionerConfig holds the settings of the provisioner component.
type ProvisionerConfig struct {
	App           GitHubApp
	Docker        Docker
	WorkerImage   string
	WorkerNetwork string
	WorkerTimeout time.Duration
	Workers       int
	QueueSize     int
}

// LoadProvisioner reads the provisioner settings through getenv and readFile, applying defaults
// and returning an error on missing or invalid values.
func LoadProvisioner(getenv func(string) string, readFile func(string) ([]byte, error)) (ProvisionerConfig, error) {
	app, err := LoadGitHubApp(getenv, readFile)
	if err != nil {
		return ProvisionerConfig{}, err
	}
	cfg := ProvisionerConfig{App: app, Docker: loadDocker(getenv), WorkerImage: DefaultWorkerImage}

	if v := getenv("MAYFLY_WORKER_IMAGE"); v != "" {
		cfg.WorkerImage = v
	}
	cfg.WorkerNetwork = getenv("MAYFLY_WORKER_NETWORK")
	if cfg.WorkerTimeout, err = loadWorkerTimeout(getenv); err != nil {
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
