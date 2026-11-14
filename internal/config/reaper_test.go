package config

import (
	"testing"
	"time"
)

// TestLoadReaper checks defaults, overrides, and that a max age not above the worker timeout is rejected.
func TestLoadReaper(t *testing.T) {
	files := fakeFS(map[string]string{"/key.pem": "PEM"})
	env := func(extra map[string]string) func(string) string {
		m := map[string]string{"MAYFLY_APP_ID": "1", "MAYFLY_APP_KEY_FILE": "/key.pem"}
		for k, v := range extra {
			m[k] = v
		}
		return func(k string) string { return m[k] }
	}

	got, err := LoadReaper(env(nil), files)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != time.Minute || got.MaxAge != 6*time.Hour+15*time.Minute || got.Grace != 2*time.Minute ||
		got.MissedProbes != 2 || got.HealthURL != "http://127.0.0.1:8081/healthz" || got.App.ID != 1 || got.Docker.Socket != DefaultDockerSocket ||
		got.StaleAfter != 10*time.Minute || got.StuckAfter != 10*time.Minute || len(got.RunnerLabels) != 1 || got.RunnerLabels[0] != "mayfly" {
		t.Errorf("defaults = %+v", got)
	}

	got, err = LoadReaper(env(map[string]string{
		"MAYFLY_WORKER_TIMEOUT": "1h", "MAYFLY_REAPER_INTERVAL": "30s", "MAYFLY_REAPER_GRACE": "5m",
		"MAYFLY_REAPER_MISSED_PROBES": "3", "MAYFLY_HEALTH_URL": "http://h/healthz",
		"MAYFLY_REAPER_STALE_AFTER": "20m", "MAYFLY_REAPER_STUCK_AFTER": "30m", "MAYFLY_RUNNER_LABELS": "mayfly,gpu",
	}), files)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxAge != time.Hour+15*time.Minute || got.Interval != 30*time.Second || got.Grace != 5*time.Minute ||
		got.MissedProbes != 3 || got.HealthURL != "http://h/healthz" ||
		got.StaleAfter != 20*time.Minute || got.StuckAfter != 30*time.Minute || len(got.RunnerLabels) != 2 {
		t.Errorf("overrides = %+v", got)
	}

	for name, extra := range map[string]map[string]string{
		"max age equals timeout": {"MAYFLY_WORKER_TIMEOUT": "1h", "MAYFLY_WORKER_MAX_AGE": "1h"},
		"max age below timeout":  {"MAYFLY_WORKER_MAX_AGE": "10m"},
		"bad interval":           {"MAYFLY_REAPER_INTERVAL": "soon"},
		"zero missed probes":     {"MAYFLY_REAPER_MISSED_PROBES": "0"},
		"bad stale after":        {"MAYFLY_REAPER_STALE_AFTER": "later"},
		"empty label":            {"MAYFLY_RUNNER_LABELS": "mayfly,,gpu"},
		"missing app id":         {"MAYFLY_APP_ID": ""},
	} {
		if _, err := LoadReaper(env(extra), files); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// TestLoadReaperIgnoresProvisionerSettings checks that a setting only the provisioner reads cannot break the reaper.
func TestLoadReaperIgnoresProvisionerSettings(t *testing.T) {
	files := fakeFS(map[string]string{"/key.pem": "PEM"})
	env := map[string]string{
		"MAYFLY_APP_ID": "1", "MAYFLY_APP_KEY_FILE": "/key.pem",
		"MAYFLY_WORKERS": "zero", "MAYFLY_QUEUE_SIZE": "-1",
	}
	getenv := func(k string) string { return env[k] }

	if _, err := LoadProvisioner(getenv, files); err == nil {
		t.Fatal("LoadProvisioner accepted invalid provisioner settings, so this test proves nothing")
	}
	if _, err := LoadReaper(getenv, files); err != nil {
		t.Errorf("LoadReaper failed on provisioner-only settings: %v", err)
	}
}
