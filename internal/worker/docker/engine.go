// Package docker runs Mayfly workers as containers through the Docker Engine API.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

// apiVersion is pinned so behaviour does not change with the daemon; every Docker since 2023 supports it.
const apiVersion = "v1.44"

// engine is a minimal Docker Engine API client.
type engine struct {
	http *http.Client
	base string
}

// engineError is a non-2xx answer from the Docker daemon.
type engineError struct {
	Op      string
	Status  int
	Message string
}

// Error implements error.
func (e *engineError) Error() string {
	return fmt.Sprintf("docker %s: %d %s", e.Op, e.Status, e.Message)
}

// Temporary reports whether the daemon failed on its side (5xx), so the same request may work later.
func (e *engineError) Temporary() bool {
	return e.Status >= http.StatusInternalServerError
}

// connError is a request that got no answer from the daemon, for example while it restarts.
type connError struct{ err error }

// Error implements error.
func (e *connError) Error() string { return e.err.Error() }

// Unwrap returns the underlying transport error.
func (e *connError) Unwrap() error { return e.err }

// Temporary reports true: the daemon may be back on the next attempt.
func (e *connError) Temporary() bool { return true }

// isNotFound reports whether err is a 404 from the daemon.
func isNotFound(err error) bool {
	var e *engineError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// newSocketEngine returns an engine that talks to the daemon on the Unix socket at path.
func newSocketEngine(path string) *engine {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}
	// The host part is ignored by the Unix dialer; "docker" just makes the URL valid.
	return &engine{http: &http.Client{Transport: tr}, base: "http://docker/" + apiVersion}
}

// containerSpec is the create request body: the subset of fields Mayfly sets.
type containerSpec struct {
	Image      string            `json:"Image"`
	Cmd        []string          `json:"Cmd"`
	Env        []string          `json:"Env"`
	Labels     map[string]string `json:"Labels"`
	HostConfig struct {
		AutoRemove bool `json:"AutoRemove"`
	} `json:"HostConfig"`
}

// imageExists reports whether image is present locally.
func (e *engine) imageExists(ctx context.Context, image string) (bool, error) {
	err := e.call(ctx, "inspect image", http.MethodGet, "/images/"+image+"/json", nil, nil, http.StatusOK)
	if isNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// pullImage downloads image, returning the first error reported in the progress stream.
func (e *engine) pullImage(ctx context.Context, image string) error {
	resp, err := e.send(ctx, http.MethodPost, "/images/create?fromImage="+url.QueryEscape(image), nil)
	if err != nil {
		return fmt.Errorf("docker pull: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return readEngineError("pull", resp)
	}
	// A pull answers 200 immediately; failures arrive later as {"error": ...} lines in the stream.
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var msg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Error != "" {
			return fmt.Errorf("docker pull %s: %s", image, msg.Error)
		}
	}
	return sc.Err()
}

// createContainer creates a container called name and returns its ID.
func (e *engine) createContainer(ctx context.Context, name string, spec containerSpec) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	path := "/containers/create?name=" + url.QueryEscape(name)
	if err := e.call(ctx, "create", http.MethodPost, path, spec, &out, http.StatusCreated); err != nil {
		return "", err
	}
	return out.ID, nil
}

// waitRemoved registers a wait for the container's removal and returns once the daemon accepted it.
// The returned function blocks until removal and reports the exit code.
func (e *engine) waitRemoved(ctx context.Context, id string) (func() (int, error), error) {
	resp, err := e.send(ctx, http.MethodPost, "/containers/"+id+"/wait?condition=removed", nil)
	if err != nil {
		return nil, &connError{fmt.Errorf("docker wait: %w", err)}
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, readEngineError("wait", resp)
	}
	return func() (int, error) {
		defer func() { _ = resp.Body.Close() }()
		var out struct {
			StatusCode int `json:"StatusCode"`
			Error      *struct {
				Message string `json:"Message"`
			} `json:"Error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return 0, fmt.Errorf("docker wait: %w", err)
		}
		if out.Error != nil && out.Error.Message != "" {
			return out.StatusCode, fmt.Errorf("docker wait: %s", out.Error.Message)
		}
		return out.StatusCode, nil
	}, nil
}

// startContainer starts the container id.
func (e *engine) startContainer(ctx context.Context, id string) error {
	return e.call(ctx, "start", http.MethodPost, "/containers/"+id+"/start", nil, nil, http.StatusNoContent)
}

// removeContainer force-removes the container id; a container that is already gone is not an error.
func (e *engine) removeContainer(ctx context.Context, id string) error {
	err := e.call(ctx, "remove", http.MethodDelete, "/containers/"+id+"?force=true", nil, nil, http.StatusNoContent)
	if isNotFound(err) {
		return nil
	}
	var ee *engineError
	if errors.As(err, &ee) && ee.Status == http.StatusConflict {
		// 409 means AutoRemove is already deleting it.
		return nil
	}
	return err
}

// containerExists reports whether a container with this ID or name exists.
func (e *engine) containerExists(ctx context.Context, idOrName string) (bool, error) {
	err := e.call(ctx, "inspect container", http.MethodGet, "/containers/"+idOrName+"/json", nil, nil, http.StatusOK)
	if isNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// call sends a JSON request and decodes the response into out when the status equals want.
func (e *engine) call(ctx context.Context, op, method, path string, in, out any, want int) error {
	resp, err := e.send(ctx, method, path, in)
	if err != nil {
		return &connError{fmt.Errorf("docker %s: %w", op, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		return readEngineError(op, resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker %s: decode: %w", op, err)
	}
	return nil
}

// send issues a request with an optional JSON body and returns the raw response.
func (e *engine) send(ctx context.Context, method, path string, in any) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return e.http.Do(req)
}

// readEngineError builds an engineError from a daemon error response.
func readEngineError(op string, resp *http.Response) error {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&msg)
	return &engineError{Op: op, Status: resp.StatusCode, Message: msg.Message}
}
