// Command mayfly runs the receiver and the provisioner in one process, joined by an in-memory job queue,
// with the ops endpoints on their own listener.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/ops"
	"github.com/TeddyBertrand/Mayfly/internal/provisioner"
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

const (
	// serverWebhook and serverOps name the two HTTP servers in logs.
	serverWebhook = "webhook"
	serverOps     = "ops"
)

// main wires config, the GitHub client, the Docker backend, the provisioner and the receiver,
// and runs them until SIGTERM or SIGINT.
func main() {
	args := os.Args[1:]
	if printVersion(args, os.Stdout) {
		return
	}
	if len(args) == 1 && args[0] == "--healthcheck" {
		os.Exit(runHealthcheck())
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("mayfly starting", logkey.Version, version)
	if err := run(logger); err != nil {
		logger.Error("mayfly failed", logkey.Err, err)
		os.Exit(1)
	}
}

// run builds every component and blocks until shutdown completes.
func run(logger *slog.Logger) error {
	rcfg, err := config.LoadReceiver(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	pcfg, err := config.LoadProvisioner(os.Getenv, os.ReadFile, version)
	if err != nil {
		return err
	}
	logger.Info("worker image selected", logkey.Image, pcfg.WorkerImage)
	key, err := github.ParsePrivateKey(pcfg.App.Key)
	if err != nil {
		return err
	}

	instance, err := newInstanceID()
	if err != nil {
		return err
	}
	logger = logger.With(logkey.Instance, instance)

	reg := metrics.NewRegistry()
	m := metrics.NewMayfly(reg)
	gh := github.NewClient(pcfg.App.APIURL, pcfg.App.ID, key)
	backend := docker.New(docker.Options{
		Socket: pcfg.Docker.Socket, Image: pcfg.WorkerImage, Network: pcfg.WorkerNetwork,
		Timeout: pcfg.WorkerTimeout, Owner: instance, Metrics: m,
	}, logger)
	prov := provisioner.New(gh, backend, provisioner.Options{
		Labels: rcfg.RunnerLabels, Workers: pcfg.Workers, QueueSize: pcfg.QueueSize, Metrics: m,
		Pool: provisioner.PoolOptions{Repos: pcfg.PoolRepos, Size: pcfg.PoolSize, Installations: gh},
	}, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	provDone := make(chan struct{})
	go func() {
		prov.Run(ctx)
		close(provDone)
	}()

	opsErr := make(chan error, 1)
	go func() {
		opsErr <- ops.Run(ctx, config.LoadOpsAddr(os.Getenv), rcfg.ShutdownTimeout,
			ops.NewHandler(instance, metrics.Handler(reg)), logger.With(logkey.Server, serverOps))
		// Stop everything if the ops server failed on its own, e.g. the port was taken.
		stop()
	}()

	err = receiver.Run(ctx, rcfg, logger.With(logkey.Server, serverWebhook), receiver.Handoff{OnQueued: prov.Enqueue, OnUpdate: prov.Update})
	// Stop the provisioner and the ops server too if the receiver failed on its own.
	stop()
	<-provDone
	return errors.Join(err, <-opsErr)
}
