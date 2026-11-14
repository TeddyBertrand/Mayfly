package config

import (
	"fmt"
	"time"
)

// Reaper defaults.
const (
	// maxAgeMargin is added to the worker timeout for the default max age, so the reaper never races the backend's own timeout.
	maxAgeMargin = 15 * time.Minute
	// DefaultReaperInterval is the time between sweeps.
	DefaultReaperInterval = time.Minute
	// DefaultReaperGrace is how long a new worker skips the GitHub runner check, while its runner boots.
	DefaultReaperGrace = 2 * time.Minute
	// DefaultMissedProbes is how many sweeps in a row the owner must miss before its workers are removed.
	DefaultMissedProbes = 2
	// DefaultStaleAfter is how long an offline registration with no container is kept; it must outlast an image pull.
	DefaultStaleAfter = 10 * time.Minute
	// DefaultStuckAfter is how long a job may wait in queued before it is logged as stuck (decision 013).
	DefaultStuckAfter = 10 * time.Minute
)

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
	cfg := ReaperConfig{App: app, Docker: loadDocker(getenv), HealthURL: DefaultHealthURL}

	if cfg.WorkerTimeout, err = loadWorkerTimeout(getenv); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.Interval, err = parseDuration(getenv, "MAYFLY_REAPER_INTERVAL", DefaultReaperInterval); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge, err = parseDuration(getenv, "MAYFLY_WORKER_MAX_AGE", cfg.WorkerTimeout+maxAgeMargin); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MaxAge <= cfg.WorkerTimeout {
		return ReaperConfig{}, fmt.Errorf("MAYFLY_WORKER_MAX_AGE: must be longer than MAYFLY_WORKER_TIMEOUT (%s), got %s", cfg.WorkerTimeout, cfg.MaxAge)
	}
	if cfg.Grace, err = parseDuration(getenv, "MAYFLY_REAPER_GRACE", DefaultReaperGrace); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.MissedProbes, err = parsePositiveInt(getenv, "MAYFLY_REAPER_MISSED_PROBES", DefaultMissedProbes); err != nil {
		return ReaperConfig{}, err
	}
	if v := getenv("MAYFLY_HEALTH_URL"); v != "" {
		cfg.HealthURL = v
	}
	if cfg.StaleAfter, err = parseDuration(getenv, "MAYFLY_REAPER_STALE_AFTER", DefaultStaleAfter); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.StuckAfter, err = parseDuration(getenv, "MAYFLY_REAPER_STUCK_AFTER", DefaultStuckAfter); err != nil {
		return ReaperConfig{}, err
	}
	if cfg.RunnerLabels, err = loadRunnerLabels(getenv); err != nil {
		return ReaperConfig{}, err
	}
	return cfg, nil
}
