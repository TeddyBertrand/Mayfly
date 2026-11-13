// Command reaper removes orphaned Mayfly workers. It runs apart from mayfly so cleanup does not depend on the process that may have failed.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/reaper"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// healthTimeout bounds the owner probe so a hung receiver counts as a missed probe instead of stalling the sweep.
const healthTimeout = 5 * time.Second

// main loads config and runs the reaper loop until SIGTERM or SIGINT.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "reaper")
	if err := run(logger); err != nil {
		logger.Error("reaper failed", "err", err)
		os.Exit(1)
	}
}

// run wires Docker, GitHub and the owner probe into a Reaper and blocks until shutdown.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadReaper(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	key, err := github.ParsePrivateKey(cfg.AppKey)
	if err != nil {
		return err
	}

	r := reaper.New(
		docker.NewInventory(cfg.DockerSocket),
		github.NewClient(cfg.GitHubAPIURL, cfg.AppID, key),
		reaper.HealthOwner(&http.Client{Timeout: healthTimeout}, cfg.HealthURL),
		reaper.Options{
			MaxAge: cfg.MaxAge, Grace: cfg.Grace, MissedProbes: cfg.MissedProbes,
			StaleAfter: cfg.StaleAfter, StuckAfter: cfg.StuckAfter, Labels: cfg.RunnerLabels,
		},
		logger,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	logger.Info("reaper starting", "interval", cfg.Interval.String(), "max_age", cfg.MaxAge.String(), "health_url", cfg.HealthURL)
	r.Run(ctx, cfg.Interval)
	logger.Info("reaper stopped")
	return nil
}
