// Package config loads Mayfly settings from MAYFLY_-prefixed environment variables.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Receiver defaults and list format.
const (
	// DefaultListenAddr is where the webhook listens; the only port exposed to GitHub (decision 018).
	DefaultListenAddr = ":8080"
	// DefaultShutdownTimeout is how long in-flight HTTP requests may drain on SIGTERM.
	DefaultShutdownTimeout = 10 * time.Second
	// DefaultRunnerLabel is the label a job must request to be Mayfly's when MAYFLY_RUNNER_LABELS is unset (decision 007).
	DefaultRunnerLabel = "mayfly"
	// labelSeparator splits MAYFLY_RUNNER_LABELS.
	labelSeparator = ","
)

// ReceiverConfig holds the settings of the receiver component.
type ReceiverConfig struct {
	ListenAddr      string
	ShutdownTimeout time.Duration
	WebhookSecret   []byte
	RunnerLabels    []string
}

// LoadReceiver reads the receiver settings through getenv and readFile, applying defaults
// and returning an error on missing or invalid values.
func LoadReceiver(getenv func(string) string, readFile func(string) ([]byte, error)) (ReceiverConfig, error) {
	cfg := ReceiverConfig{
		ListenAddr:      DefaultListenAddr,
		ShutdownTimeout: DefaultShutdownTimeout,
	}

	if v := getenv("MAYFLY_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	d, err := parseDuration(getenv, "MAYFLY_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout)
	if err != nil {
		return ReceiverConfig{}, err
	}
	cfg.ShutdownTimeout = d

	secret, err := loadSecretFile(getenv, readFile, "MAYFLY_WEBHOOK_SECRET_FILE")
	if err != nil {
		return ReceiverConfig{}, err
	}
	cfg.WebhookSecret = secret

	if cfg.RunnerLabels, err = loadRunnerLabels(getenv); err != nil {
		return ReceiverConfig{}, err
	}
	return cfg, nil
}

// loadRunnerLabels reads MAYFLY_RUNNER_LABELS, the labels a job must request to be Mayfly's.
func loadRunnerLabels(getenv func(string) string) ([]string, error) {
	labels, err := parseLabels(getenv("MAYFLY_RUNNER_LABELS"))
	if err != nil {
		return nil, fmt.Errorf("MAYFLY_RUNNER_LABELS: %w", err)
	}
	return labels, nil
}

// parseLabels splits a comma-separated label list, defaulting to DefaultRunnerLabel when v is empty.
func parseLabels(v string) ([]string, error) {
	if v == "" {
		return []string{DefaultRunnerLabel}, nil
	}
	parts := strings.Split(v, labelSeparator)
	labels := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, errors.New("empty label")
		}
		labels = append(labels, p)
	}
	return labels, nil
}

// loadSecretFile reads the secret at the path named by env, trimming surrounding whitespace.
// The error never includes the file contents.
func loadSecretFile(getenv func(string) string, readFile func(string) ([]byte, error), env string) ([]byte, error) {
	path := getenv(env)
	if path == "" {
		return nil, fmt.Errorf("%s: required", env)
	}
	b, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", env, err)
	}
	// Tools like openssl and echo add a trailing newline that would break every HMAC.
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, fmt.Errorf("%s: %w", env, errors.New("file is empty"))
	}
	return b, nil
}
