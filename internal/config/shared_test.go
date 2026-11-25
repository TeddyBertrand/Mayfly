package config

import "testing"

// TestDefaultWorkerImage checks a release gets its own worker image and a development build the stock runner.
func TestDefaultWorkerImage(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"release", "0.4.0", "ghcr.io/teddybertrand/mayfly-worker:0.4.0"},
		{"development build", DevVersion, StockWorkerImage},
		{"unstamped build", "", StockWorkerImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultWorkerImage(tt.version); got != tt.want {
				t.Errorf("DefaultWorkerImage(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

// TestLoadOpsAddr checks the ops listener defaults to loopback and honours MAYFLY_OPS_ADDR.
func TestLoadOpsAddr(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default stays on loopback", nil, "127.0.0.1:8081"},
		{"override", map[string]string{"MAYFLY_OPS_ADDR": ":9100"}, ":9100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LoadOpsAddr(func(k string) string { return tt.env[k] }); got != tt.want {
				t.Errorf("LoadOpsAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
