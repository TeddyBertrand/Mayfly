package config

import (
	"fmt"
	"time"
)

// maxAgeMargin is added to the worker timeout for the default max age, so the reaper never races the backend's own timeout.
const maxAgeMargin = 15 * time.Minute

// ReaperConfig holds the settings of the reaper. It shares the GitHub App and Docker settings with the provisioner.
type ReaperConfig struct {
	ProvisionerConfig
	Interval     time.Duration
	MaxAge       time.Duration
	Grace        time.Duration
	MissedProbes int
	HealthURL    string
}

// LoadReaper reads the reaper settings through getenv and readFile. MAYFLY_WORKER_MAX_AGE must exceed
// MAYFLY_WORKER_TIMEOUT, or the reaper would kill jobs the backend still allows to run.
func LoadReaper(getenv func(string) string, readFile func(string) ([]byte, error)) (ReaperConfig, error) {
	p, err := LoadProvisioner(getenv, readFile)
	if err != nil {
		return ReaperConfig{}, err
	}
	cfg := ReaperConfig{ProvisionerConfig: p, HealthURL: "http://127.0.0.1:8080/healthz"}

	if cfg.Interval, err = parseDuration(getenv, "MAYFLY_REAPER_INTERVAL", time.Minute); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge, err = parseDuration(getenv, "MAYFLY_WORKER_MAX_AGE", p.WorkerTimeout+maxAgeMargin); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge <= p.WorkerTimeout {
		return ReaperConfig{}, fmt.Errorf("MAYFLY_WORKER_MAX_AGE: must be longer than MAYFLY_WORKER_TIMEOUT (%s), got %s", p.WorkerTimeout, cfg.MaxAge)
	}
	if cfg.Grace, err = parseDuration(getenv, "MAYFLY_REAPER_GRACE", 2*time.Minute); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MissedProbes, err = parsePositiveInt(getenv, "MAYFLY_REAPER_MISSED_PROBES", 2); err != nil {
		return ReaperConfig{}, err
	}
	if v := getenv("MAYFLY_HEALTH_URL"); v != "" {
		cfg.HealthURL = v
	}
	return cfg, nil
}
