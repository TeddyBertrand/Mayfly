package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the public GitHub REST API.
	DefaultBaseURL = "https://api.github.com"
	// tokenRefreshMargin renews a token early so it cannot expire mid-request.
	tokenRefreshMargin = 5 * time.Minute
)

// Client calls the GitHub REST API as a GitHub App, caching installation tokens.
type Client struct {
	baseURL string
	appID   int64
	key     *rsa.PrivateKey
	http    *http.Client
	now     func() time.Time

	mu     sync.Mutex
	tokens map[int64]installationToken
}

// installationToken is a cached token and its expiry time.
type installationToken struct {
	value     string
	expiresAt time.Time
}

// APIError is a non-2xx response from GitHub. It never contains credentials.
type APIError struct {
	Method  string
	Path    string
	Status  int
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	return fmt.Sprintf("github %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// NewClient returns a Client for appID that signs with key and talks to baseURL.
func NewClient(baseURL string, appID int64, key *rsa.PrivateKey) *Client {
	return &Client{
		baseURL: baseURL,
		appID:   appID,
		key:     key,
		http:    &http.Client{Timeout: 10 * time.Second},
		now:     time.Now,
		tokens:  make(map[int64]installationToken),
	}
}

// InstallationToken returns a token for installationID, reusing the cached one until it nears expiry.
func (c *Client) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	// Held across the refresh so concurrent callers wait for one request instead of all refreshing.
	c.mu.Lock()
	defer c.mu.Unlock()

	if tok, ok := c.tokens[installationID]; ok && c.now().Add(tokenRefreshMargin).Before(tok.expiresAt) {
		return tok.value, nil
	}

	jwt, err := appJWT(c.appID, c.key, c.now())
	if err != nil {
		return "", err
	}
	var resp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installationID)
	if err := c.do(ctx, http.MethodPost, path, jwt, nil, &resp, http.StatusCreated); err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("github POST %s: empty token in response", path)
	}

	c.tokens[installationID] = installationToken{value: resp.Token, expiresAt: resp.ExpiresAt}
	return resp.Token, nil
}

// do sends a JSON request authenticated with bearer and decodes the response into out
// when the status equals want.
func (c *Client) do(ctx context.Context, method, path, bearer string, in, out any, want int) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("github %s %s: encode: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != want {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		return &APIError{Method: method, Path: path, Status: resp.StatusCode, Message: e.Message}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github %s %s: decode: %w", method, path, err)
	}
	return nil
}
