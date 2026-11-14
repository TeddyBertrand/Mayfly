package config

import (
	"fmt"
	"time"
)

// maxAgeMargin is added to the worker timeout for the default max age, so the reaper never races the backend's own timeout.
const maxAgeMargin = 15 * time.Minute

// ReaperConfig holds the settings of the reaper: the shared GitHub App and Docker settings plus its own.
type ReaperConfig struct {
	App    GitHubApp
	Docker Docker
	// WorkerTimeout is read only to check that MaxAge outlasts it.
	WorkerTimeout time.Duration
	Interval      time.Duration
	MaxAge        time.Duration
	Grace         time.Duration
	MissedProbes  int
	HealthURL     string
	StaleAfter    time.Duration
	StuckAfter    time.Duration
	RunnerLabels  []string
}

// LoadReaper reads the reaper settings through getenv and readFile. MAYFLY_WORKER_MAX_AGE must exceed
// MAYFLY_WORKER_TIMEOUT, or the reaper would kill jobs the backend still allows to run.
func LoadReaper(getenv func(string) string, readFile func(string) ([]byte, error)) (ReaperConfig, error) {
	app, err := LoadGitHubApp(getenv, readFile)
	if err != nil {
		return ReaperConfig{}, err
	}
	cfg := ReaperConfig{App: app, Docker: loadDocker(getenv), HealthURL: "http://127.0.0.1:8080/healthz"}

	if cfg.WorkerTimeout, err = loadWorkerTimeout(getenv); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.Interval, err = parseDuration(getenv, "MAYFLY_REAPER_INTERVAL", time.Minute); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge, err = parseDuration(getenv, "MAYFLY_WORKER_MAX_AGE", cfg.WorkerTimeout+maxAgeMargin); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge <= cfg.WorkerTimeout {
		return ReaperConfig{}, fmt.Errorf("MAYFLY_WORKER_MAX_AGE: must be longer than MAYFLY_WORKER_TIMEOUT (%s), got %s", cfg.WorkerTimeout, cfg.MaxAge)
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
	if cfg.StaleAfter, err = parseDuration(getenv, "MAYFLY_REAPER_STALE_AFTER", 10*time.Minute); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.StuckAfter, err = parseDuration(getenv, "MAYFLY_REAPER_STUCK_AFTER", 10*time.Minute); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.RunnerLabels, err = parseLabels(getenv("MAYFLY_RUNNER_LABELS")); err != nil {
		return ReaperConfig{}, fmt.Errorf("MAYFLY_RUNNER_LABELS: %w", err)
	}
	return cfg, nil
}
