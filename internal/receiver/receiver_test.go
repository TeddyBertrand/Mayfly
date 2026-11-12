package receiver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// discardLogger returns a logger that drops all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testWebhook returns a Webhook with the "mayfly" label, a fresh deduper, and onQueued (or a no-op).
func testWebhook(secret []byte, onQueued func(Job)) Webhook {
	if onQueued == nil {
		onQueued = func(Job) {}
	}
	return Webhook{
		Secret:   secret,
		Labels:   []string{"mayfly"},
		Dedupe:   NewMemoryDeduper(time.Hour),
		OnQueued: func(j Job) error { onQueued(j); return nil },
	}
}

// TestWebhookHandoffFailure checks that a rejected handoff answers 503 and lets the redelivery through.
func TestWebhookHandoffFailure(t *testing.T) {
	secret := []byte("test-secret")
	wh := testWebhook(secret, nil)
	fail := true
	accepted := 0
	wh.OnQueued = func(Job) error {
		if fail {
			return errors.New("queue full")
		}
		accepted++
		return nil
	}
	h := NewHandler(discardLogger(), wh)

	if got := postSigned(h, secret, "workflow_job", "d-retry", queuedPayload); got != http.StatusServiceUnavailable {
		t.Fatalf("first attempt status = %d, want 503", got)
	}
	fail = false
	if got := postSigned(h, secret, "workflow_job", "d-retry", queuedPayload); got != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202", got)
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want 1", accepted)
	}
}

// postSigned sends a signed POST /webhook with the given event and delivery headers.
func postSigned(h http.Handler, secret []byte, event, delivery, body string) int {
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Hub-Signature-256", sign(secret, []byte(body)))
	req.Header.Set("X-GitHub-Event", event)
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestWebhookDispatch checks status codes and handoffs for each kind of verified delivery.
func TestWebhookDispatch(t *testing.T) {
	secret := []byte("test-secret")
	completed := strings.Replace(queuedPayload, `"queued"`, `"completed"`, 1)
	hosted := strings.Replace(queuedPayload, `["self-hosted", "mayfly"]`, `["ubuntu-latest"]`, 1)

	tests := []struct {
		name       string
		event      string
		delivery   string
		body       string
		wantStatus int
		wantJobs   int
	}{
		{"queued for mayfly", "workflow_job", "d1", queuedPayload, http.StatusAccepted, 1},
		{"ping", "ping", "d2", `{"zen":"hi"}`, http.StatusOK, 0},
		{"unknown event", "push", "d3", `{"ref":"main"}`, http.StatusAccepted, 0},
		{"no event header", "", "d4", `{}`, http.StatusAccepted, 0},
		{"completed action", "workflow_job", "d5", completed, http.StatusAccepted, 0},
		{"github-hosted labels", "workflow_job", "d6", hosted, http.StatusAccepted, 0},
		{"malformed workflow_job", "workflow_job", "d7", `{`, http.StatusBadRequest, 0},
		{"missing delivery id", "workflow_job", "", queuedPayload, http.StatusBadRequest, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var jobs []Job
			h := NewHandler(discardLogger(), testWebhook(secret, func(j Job) { jobs = append(jobs, j) }))
			if got := postSigned(h, secret, tt.event, tt.delivery, tt.body); got != tt.wantStatus {
				t.Errorf("status = %d, want %d", got, tt.wantStatus)
			}
			if len(jobs) != tt.wantJobs {
				t.Errorf("handoffs = %d, want %d", len(jobs), tt.wantJobs)
			}
		})
	}
}

// TestWebhookSameDeliveryTwice checks that a redelivered queued job is handed off once.
func TestWebhookSameDeliveryTwice(t *testing.T) {
	secret := []byte("test-secret")
	var jobs []Job
	h := NewHandler(discardLogger(), testWebhook(secret, func(j Job) { jobs = append(jobs, j) }))

	for i := range 2 {
		if got := postSigned(h, secret, "workflow_job", "same-delivery", queuedPayload); got != http.StatusAccepted {
			t.Fatalf("attempt %d: status = %d, want 202", i+1, got)
		}
	}
	if len(jobs) != 1 {
		t.Fatalf("handoffs = %d, want 1", len(jobs))
	}
	if jobs[0].ID != 42 {
		t.Errorf("job id = %d, want 42", jobs[0].ID)
	}
}

// TestHandlerRoutes checks status codes for each route and method.
func TestHandlerRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"healthz ok", http.MethodGet, "/healthz", http.StatusOK},
		{"webhook unsigned", http.MethodPost, "/webhook", http.StatusUnauthorized},
		{"webhook wrong method", http.MethodGet, "/webhook", http.StatusMethodNotAllowed},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound},
	}

	h := NewHandler(discardLogger(), testWebhook([]byte("test-secret"), nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestWebhookSignatureAndLimit checks that only correctly signed bodies under the size limit are accepted.
func TestWebhookSignatureAndLimit(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"action":"queued"}`)
	huge := bytes.Repeat([]byte("a"), maxWebhookBody+1)

	tests := []struct {
		name   string
		body   []byte
		header string
		want   int
	}{
		{"valid", body, sign(secret, body), http.StatusAccepted},
		{"invalid signature", body, sign([]byte("wrong"), body), http.StatusUnauthorized},
		{"missing signature", body, "", http.StatusUnauthorized},
		{"tampered body", []byte(`{"action":"completed"}`), sign(secret, body), http.StatusUnauthorized},
		{"body too large", huge, sign(secret, huge), http.StatusRequestEntityTooLarge},
	}

	h := NewHandler(discardLogger(), testWebhook(secret, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(tt.body))
			if tt.header != "" {
				req.Header.Set("X-Hub-Signature-256", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestServeShutsDownOnCancel starts a real server, probes /healthz, then cancels and expects a clean return.
func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.ReceiverConfig{ShutdownTimeout: 2 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, cfg, discardLogger(), func(Job) error { return nil }) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
}
