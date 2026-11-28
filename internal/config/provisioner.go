package config

import (
	"fmt"
	"math"
	"net/url"
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
	// defaultPoolMaxFactor sets the default cap on warm runners alive per repository, as a multiple of the pool size (decision 026).
	defaultPoolMaxFactor = 2
	// DefaultPoolIdleTimeout is how long a warm runner may wait for a job before it is retired (decision 026).
	DefaultPoolIdleTimeout = 30 * time.Minute
	// repoSeparator splits MAYFLY_POOL_REPOS; ownerSeparator splits "owner/name".
	repoSeparator  = ","
	ownerSeparator = "/"
	// schemeHTTP, schemeHTTPS, urlPathSeparator and goproxySeparators validate the service URLs.
	schemeHTTP        = "http"
	schemeHTTPS       = "https"
	urlPathSeparator  = "/"
	goproxySeparators = ",|"
)

// byteUnits are the size suffixes MAYFLY_WORKER_TMPFS_SIZE accepts.
var byteUnits = map[byte]int64{'k': kib, 'm': kib * kib, 'g': kib * kib * kib}

// kib is one binary kilobyte, the step between the size units.
const kib = 1024

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
	// PoolMax caps the warm runners alive per repository, idle and busy.
	PoolMax         int
	PoolIdleTimeout time.Duration
	// CacheURL is the cache server workers send actions/cache traffic to (decision 028); empty keeps GitHub's.
	CacheURL string
	// TmpfsBytes is the memory budget for the workers' tmpfs mounts (decision 029); 0 keeps them on disk.
	TmpfsBytes int64
	// GoProxyURL and NPMRegistryURL are the package proxies workers download from (decision 030); empty keeps the public ones.
	GoProxyURL     string
	NPMRegistryURL string
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
	if cfg.PoolMax, err = parsePositiveInt(getenv, "MAYFLY_POOL_MAX", cfg.PoolSize*defaultPoolMaxFactor); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.PoolMax < cfg.PoolSize {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_POOL_MAX: must be at least MAYFLY_POOL_SIZE (%d), got %d", cfg.PoolSize, cfg.PoolMax)
	}
	if cfg.PoolIdleTimeout, err = parseDuration(getenv, "MAYFLY_POOL_IDLE_TIMEOUT", DefaultPoolIdleTimeout); err != nil {
		return ProvisionerConfig{}, err
	}
	if cfg.PoolIdleTimeout >= cfg.WorkerTimeout {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_POOL_IDLE_TIMEOUT: must be shorter than MAYFLY_WORKER_TIMEOUT (%s), got %s", cfg.WorkerTimeout, cfg.PoolIdleTimeout)
	}
	if cfg.CacheURL, err = parseServiceURL(getenv("MAYFLY_CACHE_URL")); err != nil {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_CACHE_URL: %w", err)
	}
	if cfg.GoProxyURL, err = parseServiceURL(getenv("MAYFLY_GO_PROXY_URL")); err != nil {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_GO_PROXY_URL: %w", err)
	}
	if cfg.NPMRegistryURL, err = parseServiceURL(getenv("MAYFLY_NPM_REGISTRY_URL")); err != nil {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_NPM_REGISTRY_URL: %w", err)
	}
	if cfg.TmpfsBytes, err = parseByteSize(getenv("MAYFLY_WORKER_TMPFS_SIZE")); err != nil {
		return ProvisionerConfig{}, fmt.Errorf("MAYFLY_WORKER_TMPFS_SIZE: %w", err)
	}
	return cfg, nil
}

// parseByteSize reads a positive size such as 1500m or 2g, with binary units (k, m, g) as Docker uses.
// An empty v returns 0.
func parseByteSize(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	num, mult := strings.ToLower(v), int64(1)
	if unit, ok := byteUnits[num[len(num)-1]]; ok {
		num, mult = num[:len(num)-1], unit
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/mult {
		return 0, fmt.Errorf("must be a positive size such as 1500m or 2g, got %q", v)
	}
	return n * mult, nil
}

// parseServiceURL checks that v is an absolute http(s) URL with no query, and returns it with
// a trailing slash. An empty v returns "".
func parseServiceURL(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", err
	}
	// GOPROXY reads commas and pipes as list separators, so one URL must not hold them.
	if (u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS) || u.Host == "" || u.RawQuery != "" || u.Fragment != "" ||
		strings.ContainsAny(v, goproxySeparators) {
		return "", fmt.Errorf("must be an http(s) URL with a host and no query, got %q", v)
	}
	// actions/cache joins paths onto it and the cache server requires the slash; Go and npm accept it.
	if !strings.HasSuffix(v, urlPathSeparator) {
		v += urlPathSeparator
	}
	return v, nil
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
