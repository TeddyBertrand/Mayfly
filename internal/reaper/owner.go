package reaper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// HealthOwner returns an Owner that reads the instance ID from the mayfly /healthz endpoint at url.
func HealthOwner(client *http.Client, url string) Owner {
	return func(ctx context.Context) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("healthz: status %d", resp.StatusCode)
		}
		var h struct {
			Instance string `json:"instance"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
			return "", fmt.Errorf("healthz: %w", err)
		}
		if h.Instance == "" {
			return "", errors.New("healthz: no instance in response")
		}
		return h.Instance, nil
	}
}
