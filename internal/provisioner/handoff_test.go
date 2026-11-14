package provisioner

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/receiver"
)

const queuedBody = `{"action":"queued","workflow_job":{"id":4242,"run_id":1,"name":"build","labels":["self-hosted","mayfly"]},` +
	`"repository":{"full_name":"octo/demo"},"installation":{"id":9}}`

// TestWebhookDoesNotWaitOnProvisioning wires the real receiver to a provisioner whose worker never ends,
// and checks the webhook still answers 202 quickly.
func TestWebhookDoesNotWaitOnProvisioning(t *testing.T) {
	b := newFakeBackend(true)
	p, _ := newTestProvisioner(&fakeJIT{}, b, 1, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	secret := []byte("test-secret")
	h := receiver.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), receiver.Webhook{
		Secret:  secret,
		Labels:  []string{"mayfly"},
		Dedupe:  receiver.NewMemoryDeduper(time.Hour),
		Handoff: receiver.Handoff{OnQueued: p.Enqueue, OnUpdate: p.Update},
	})

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(queuedBody))
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(queuedBody)))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-GitHub-Delivery", "d-fast")
	rec := httptest.NewRecorder()

	start := time.Now()
	h.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("webhook took %s, want well under 1s", elapsed)
	}
	waitStarted(t, b, 1)
	t.Logf("webhook answered in %s while the worker is still running", elapsed)
}
