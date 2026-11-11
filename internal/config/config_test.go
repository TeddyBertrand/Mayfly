package config

import (
	"testing"
	"time"
)

// TestLoadReceiver checks defaults, overrides and rejection of invalid values.
func TestLoadReceiver(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    ReceiverConfig
		wantErr bool
	}{
		{
			name: "defaults",
			env:  map[string]string{},
			want: ReceiverConfig{ListenAddr: ":8080", ShutdownTimeout: 10 * time.Second},
		},
		{
			name: "overrides",
			env:  map[string]string{"MAYFLY_LISTEN_ADDR": "127.0.0.1:9000", "MAYFLY_SHUTDOWN_TIMEOUT": "3s"},
			want: ReceiverConfig{ListenAddr: "127.0.0.1:9000", ShutdownTimeout: 3 * time.Second},
		},
		{
			name:    "invalid duration",
			env:     map[string]string{"MAYFLY_SHUTDOWN_TIMEOUT": "soon"},
			wantErr: true,
		},
		{
			name:    "non-positive duration",
			env:     map[string]string{"MAYFLY_SHUTDOWN_TIMEOUT": "0s"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadReceiver(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
