// Package config loads Mayfly settings from MAYFLY_-prefixed environment variables.
package config

import (
	"fmt"
	"time"
)

// ReceiverConfig holds the settings of the receiver component.
type ReceiverConfig struct {
	ListenAddr      string
	ShutdownTimeout time.Duration
}

// LoadReceiver reads the receiver settings through getenv, applying defaults
// and returning an error on invalid values.
func LoadReceiver(getenv func(string) string) (ReceiverConfig, error) {
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

	return cfg, nil
}
