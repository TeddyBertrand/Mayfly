// Package config loads Mayfly settings from MAYFLY_-prefixed environment variables.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

// ReceiverConfig holds the settings of the receiver component.
type ReceiverConfig struct {
	ListenAddr      string
	ShutdownTimeout time.Duration
	WebhookSecret   []byte
}

// LoadReceiver reads the receiver settings through getenv and readFile, applying defaults
// and returning an error on missing or invalid values.
func LoadReceiver(getenv func(string) string, readFile func(string) ([]byte, error)) (ReceiverConfig, error) {
	cfg := ReceiverConfig{
		ListenAddr:      ":8080",
		ShutdownTimeout: 10 * time.Second,
	}

	if v := getenv("MAYFLY_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	if v := getenv("MAYFLY_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return ReceiverConfig{}, fmt.Errorf("MAYFLY_SHUTDOWN_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return ReceiverConfig{}, fmt.Errorf("MAYFLY_SHUTDOWN_TIMEOUT: must be positive, got %s", d)
		}
		cfg.ShutdownTimeout = d
	}

	secret, err := loadSecretFile(getenv, readFile, "MAYFLY_WEBHOOK_SECRET_FILE")
	if err != nil {
		return ReceiverConfig{}, err
	}
	cfg.WebhookSecret = secret

	return cfg, nil
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
