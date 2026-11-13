package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthURL checks that wildcard and empty hosts are dialled on loopback.
func TestHealthURL(t *testing.T) {
	tests := []struct {
		addr, want string
		wantErr    bool
	}{
		{addr: ":8080", want: "http://127.0.0.1:8080/healthz"},
		{addr: "0.0.0.0:9000", want: "http://127.0.0.1:9000/healthz"},
		{addr: "[::]:8080", want: "http://127.0.0.1:8080/healthz"},
		{addr: "10.0.0.5:8080", want: "http://10.0.0.5:8080/healthz"},
		{addr: "8080", wantErr: true},
	}
	for _, tt := range tests {
		got, err := healthURL(tt.addr)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("healthURL(%q) = %q, %v; want %q, err %v", tt.addr, got, err, tt.want, tt.wantErr)
		}
	}
}

// TestHealthcheck checks that only a 200 answer counts as healthy.
func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()

	if err := healthcheck(srv.URL, time.Second); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := healthcheck(srv.URL, time.Second); err == nil {
		t.Error("503 reported healthy")
	}
	srv.Close()
	if err := healthcheck(srv.URL, time.Second); err == nil {
		t.Error("closed server reported healthy")
	}
}
