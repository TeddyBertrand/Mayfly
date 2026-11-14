// Command mayfly runs the receiver and the provisioner in one process, joined by an in-memory job queue,
// with the ops endpoints on their own listener.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/logkey"
	"github.com/TeddyBertrand/Mayfly/internal/ops"
	"github.com/TeddyBertrand/Mayfly/internal/provisioner"
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// version is the release version, set at build time with -ldflags "-X main.version=<v>".
var version = "dev"

const (
	// healthcheckTimeout keeps the container healthcheck well under Compose's 5s probe timeout.
	healthcheckTimeout = 3 * time.Second
	// instanceIDBytes is the random part of the instance ID; hex doubles it to 16 characters.
	instanceIDBytes = 8
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

// printVersion writes the version to w and reports true when args ask for it.
func printVersion(args []string, w io.Writer) bool {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		_, _ = fmt.Fprintln(w, version)
		return true
	}
	return false
}

// runHealthcheck probes this host's ops listener at MAYFLY_OPS_ADDR and returns the process exit code.
func runHealthcheck() int {
	url, err := healthURL(config.LoadOpsAddr(os.Getenv))
	if err == nil {
		err = healthcheck(url, healthcheckTimeout)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	return 0
}

// newInstanceID returns a random ID for this process; a restart gets a new one, which is how the reaper spots orphans.
func newInstanceID() (string, error) {
	b := make([]byte, instanceIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("instance id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// run builds every component and blocks until shutdown completes.
func run(logger *slog.Logger) error {
	rcfg, err := config.LoadReceiver(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	pcfg, err := config.LoadProvisioner(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	key, err := github.ParsePrivateKey(pcfg.App.Key)
	if err != nil {
		return err
	}

	instance, err := newInstanceID()
	if err != nil {
		return err
	}
	logger = logger.With(logkey.Instance, instance)

	gh := github.NewClient(pcfg.App.APIURL, pcfg.App.ID, key)
	backend := docker.New(docker.Options{
		Socket: pcfg.Docker.Socket, Image: pcfg.WorkerImage, Network: pcfg.WorkerNetwork,
		Timeout: pcfg.WorkerTimeout, Owner: instance,
	}, logger)
	prov := provisioner.New(gh, backend, provisioner.Options{
		Labels: rcfg.RunnerLabels, Workers: pcfg.Workers, QueueSize: pcfg.QueueSize,
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
		opsErr <- ops.Run(ctx, config.LoadOpsAddr(os.Getenv), rcfg.ShutdownTimeout, instance, logger.With(logkey.Server, serverOps))
		// Stop everything if the ops server failed on its own, e.g. the port was taken.
		stop()
	}()

	err = receiver.Run(ctx, rcfg, logger.With(logkey.Server, serverWebhook), receiver.Handoff{OnQueued: prov.Enqueue, OnUpdate: prov.Update})
	// Stop the provisioner and the ops server too if the receiver failed on its own.
	stop()
	<-provDone
	return errors.Join(err, <-opsErr)
}
