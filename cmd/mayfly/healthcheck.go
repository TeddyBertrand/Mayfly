package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// healthcheckTimeout keeps the container healthcheck well under Compose's 5s probe timeout.
const healthcheckTimeout = 3 * time.Second

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

// healthURL returns the /healthz URL for an ops listener on listenAddr, dialling loopback
// when the host part is empty or a wildcard.
func healthURL(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", listenAddr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

// healthcheck GETs url and returns an error unless it answers 200 within timeout.
// It backs the container healthcheck, since the distroless image has no curl.
func healthcheck(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return nil
}
