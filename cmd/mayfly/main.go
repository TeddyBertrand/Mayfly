// Command mayfly runs the receiver and the provisioner in one process, joined by an in-memory job queue.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/provisioner"
	"github.com/TeddyBertrand/Mayfly/internal/receiver"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// version is the release version, set at build time with -ldflags "-X main.version=<v>".
var version = "dev"

// main wires config, the GitHub client, the Docker backend, the provisioner and the receiver,
// and runs them until SIGTERM or SIGINT.
func main() {
	if printVersion(os.Args[1:], os.Stdout) {
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("mayfly starting", "version", version)
	if err := run(logger); err != nil {
		logger.Error("mayfly failed", "err", err)
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
	key, err := github.ParsePrivateKey(pcfg.AppKey)
	if err != nil {
		return err
	}

	gh := github.NewClient(pcfg.GitHubAPIURL, pcfg.AppID, key)
	backend := docker.New(pcfg.DockerSocket, pcfg.WorkerImage, pcfg.WorkerTimeout, logger)
	prov := provisioner.New(gh, backend, rcfg.RunnerLabels, pcfg.Workers, pcfg.QueueSize, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	provDone := make(chan struct{})
	go func() {
		prov.Run(ctx)
		close(provDone)
	}()

	err = receiver.Run(ctx, rcfg, logger, prov.Enqueue)
	// Stop the provisioner too if the receiver failed on its own, e.g. the port was taken.
	stop()
	<-provDone
	return err
}
