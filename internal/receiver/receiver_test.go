package receiver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
)

// discardLogger returns a logger that drops all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

	h := NewHandler(discardLogger(), []byte("test-secret"))
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

	h := NewHandler(discardLogger(), secret)
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
	go func() { done <- serve(ctx, ln, cfg, discardLogger()) }()

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
