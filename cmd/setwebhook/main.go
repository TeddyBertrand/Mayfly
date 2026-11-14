// Command setwebhook points the Mayfly GitHub App's webhook at a new URL. It is a local dev tool,
// used by scripts/dev.sh after a quick tunnel starts.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/github"
)

// main sets the App webhook URL to os.Args[1] and exits non-zero on failure.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("set webhook url failed", "err", err)
		os.Exit(1)
	}
}

// run reads the App credentials from the MAYFLY_* environment, logs the current URL and replaces it.
func run(args []string, logger *slog.Logger) error {
	if len(args) != 1 {
		return errors.New("usage: setwebhook https://<host>/webhook")
	}
	app, err := config.LoadGitHubApp(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	key, err := github.ParsePrivateKey(app.Key)
	if err != nil {
		return err
	}
	c := github.NewClient(app.APIURL, app.ID, key)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	old, err := c.AppWebhookURL(ctx)
	if err != nil {
		return fmt.Errorf("read current url: %w", err)
	}
	if old == args[0] {
		logger.Info("webhook url already set", "url", old)
		return nil
	}
	if err := c.SetAppWebhookURL(ctx, args[0]); err != nil {
		return err
	}
	logger.Info("webhook url updated", "old", old, "new", args[0])
	return nil
}
