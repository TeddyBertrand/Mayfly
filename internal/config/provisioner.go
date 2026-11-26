package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Provisioner defaults.
const (
	// DefaultWorkers is how many jobs are provisioned at once.
	DefaultWorkers = 2
	// DefaultQueueSize is how many jobs may wait before the receiver answers 503.
	DefaultQueueSize = 100
	// DefaultPoolSize is how many idle warm runners each pooled repository keeps (decision 025).
	DefaultPoolSize = 1
	// repoSeparator splits MAYFLY_POOL_REPOS; ownerSeparator splits "owner/name".
	repoSeparator  = ","
	ownerSeparator = "/"
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
	// PoolRepos are the repositories that keep warm runners; empty disables the pool.
	PoolRepos []string
	PoolSize  int
}

// LoadProvisioner reads the provisioner settings through getenv and readFile, applying defaults
// and returning an error on missing or invalid values. version is the binary's release version,
// which picks the default worker image.
func LoadProvisioner(getenv func(string) string, readFile func(string) ([]byte, error), version string) (ProvisionerConfig, error) {
	app, err := LoadGitHubApp(getenv, readFile)
	if err != nil {
		return ProvisionerConfig{}, err
	}
	cfg := ProvisionerConfig{App: app, Docker: loadDocker(getenv), WorkerImage: DefaultWorkerImage(version)}

	if v := getenv("MAYFLY_WORKER_IMAGE"); v != "" {
		cfg.WorkerImage = v
	}
	cfg.WorkerNetwork = getenv("MAYFLY_WORKER_NETWORK")
	if cfg.WorkerTimeout, err = loadWorkerTimeout(getenv); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.Workers, err = parsePositiveInt(getenv, "MAYFLY_WORKERS", DefaultWorkers); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.QueueSize, err = parsePositiveInt(getenv, "MAYFLY_QUEUE_SIZE", DefaultQueueSize); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.PoolRepos, err = parseRepos(getenv("MAYFLY_POOL_REPOS")); err != nil {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_POOL_REPOS: %w", err)
	}
	if cfg.PoolSize, err = parsePositiveInt(getenv, "MAYFLY_POOL_SIZE", DefaultPoolSize); err != nil {
		return ProvisionerConfig{}, err
	}
	return cfg, nil
}

// parseRepos splits a comma-separated list of "owner/name" repositories, dropping duplicates.
// An empty v returns nil.
func parseRepos(v string) ([]string, error) {
	if v == "" {
		return nil, nil
	}
	var repos []string
	seen := map[string]bool{}
	for _, p := range strings.Split(v, repoSeparator) {
		p = strings.TrimSpace(p)
		owner, name, ok := strings.Cut(p, ownerSeparator)
		if !ok || owner == "" || name == "" || strings.Contains(name, ownerSeparator) {
			return nil, fmt.Errorf("%q is not owner/name", p)
		}
		// GitHub repository names are case-insensitive.
		if key := strings.ToLower(p); !seen[key] {
			seen[key] = true
			repos = append(repos, p)
		}
	}
	return repos, nil
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
