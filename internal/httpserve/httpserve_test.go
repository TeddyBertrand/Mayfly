package httpserve

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeShutsDownOnCancel starts a real server, makes a request, then cancels and expects a clean return.
func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, srv, ln, 2*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}
