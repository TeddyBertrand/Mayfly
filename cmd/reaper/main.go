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
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/reaper"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

const (
	// healthTimeout bounds the owner probe so a hung receiver counts as a missed probe instead of stalling the sweep.
	healthTimeout = 5 * time.Second
	// component names this process in logs.
	component = "reaper"
)

// main loads config and runs the reaper loop until SIGTERM or SIGINT.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(logkey.Component, component)
	if err := run(logger); err != nil {
		logger.Error("reaper failed", logkey.Err, err)
		os.Exit(1)
	}
}

// run wires Docker, GitHub and the owner probe into a Reaper and blocks until shutdown.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadReaper(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	key, err := github.ParsePrivateKey(cfg.App.Key)
	if err != nil {
		return err
	}

	r := reaper.New(
		docker.NewInventory(cfg.Docker.Socket),
		github.NewClient(cfg.App.APIURL, cfg.App.ID, key),
		reaper.HealthOwner(&http.Client{Timeout: healthTimeout}, cfg.HealthURL),
		reaper.Options{
			MaxAge: cfg.MaxAge, Grace: cfg.Grace, MissedProbes: cfg.MissedProbes,
			StaleAfter: cfg.StaleAfter, StuckAfter: cfg.StuckAfter, Labels: cfg.RunnerLabels,
		},
		logger,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	logger.Info("reaper starting", logkey.Interval, cfg.Interval.String(), logkey.MaxAge, cfg.MaxAge.String(), logkey.HealthURL, cfg.HealthURL)
	r.Run(ctx, cfg.Interval)
	logger.Info("reaper stopped")
	return nil
}
