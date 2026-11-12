package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// AppWebhookURL returns the webhook URL currently configured on the App.
func (c *Client) AppWebhookURL(ctx context.Context) (string, error) {
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return "", err
	}
	var cfg struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodGet, "/app/hook/config", jwt, nil, &cfg, http.StatusOK); err != nil {
		return "", err
	}
	return cfg.URL, nil
}

// SetAppWebhookURL points the App's webhook at rawURL, which must be absolute https.
// Only the URL is sent, so the webhook secret and other settings are left unchanged.
func (c *Client) SetAppWebhookURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("set webhook url: %q is not an absolute https URL", rawURL)
	}
	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return err
	}
	body := struct {
		URL string `json:"url"`
	}{rawURL}
	return c.do(ctx, http.MethodPatch, "/app/hook/config", jwt, body, nil, http.StatusOK)
}
