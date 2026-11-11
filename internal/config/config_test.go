package config

import (
	"io/fs"
	"reflect"
	"testing"
	"time"
)

// fakeFS returns a readFile function serving files from an in-memory map.
func fakeFS(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		v, ok := files[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(v), nil
	}
}

// TestLoadReceiver checks defaults, overrides, secret loading and rejection of invalid values.
func TestLoadReceiver(t *testing.T) {
	secretEnv := map[string]string{"MAYFLY_WEBHOOK_SECRET_FILE": "/secret"}
	secretFS := fakeFS(map[string]string{"/secret": "test-secret\n", "/empty": " \n"})

	tests := []struct {
		name    string
		env     map[string]string
		want    ReceiverConfig
		wantErr bool
	}{
		{
			name: "defaults with trimmed secret",
			env:  secretEnv,
			want: ReceiverConfig{ListenAddr: ":8080", ShutdownTimeout: 10 * time.Second, WebhookSecret: []byte("test-secret")},
		},
		{
			name: "overrides",
			env:  map[string]string{"MAYFLY_LISTEN_ADDR": "127.0.0.1:9000", "MAYFLY_SHUTDOWN_TIMEOUT": "3s", "MAYFLY_WEBHOOK_SECRET_FILE": "/secret"},
			want: ReceiverConfig{ListenAddr: "127.0.0.1:9000", ShutdownTimeout: 3 * time.Second, WebhookSecret: []byte("test-secret")},
		},
		{
			name:    "invalid duration",
			env:     map[string]string{"MAYFLY_SHUTDOWN_TIMEOUT": "soon", "MAYFLY_WEBHOOK_SECRET_FILE": "/secret"},
			wantErr: true,
		},
		{
			name:    "non-positive duration",
			env:     map[string]string{"MAYFLY_SHUTDOWN_TIMEOUT": "0s", "MAYFLY_WEBHOOK_SECRET_FILE": "/secret"},
			wantErr: true,
		},
		{
			name:    "missing secret file env",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name:    "secret file not found",
			env:     map[string]string{"MAYFLY_WEBHOOK_SECRET_FILE": "/nope"},
			wantErr: true,
		},
		{
			name:    "empty secret file",
			env:     map[string]string{"MAYFLY_WEBHOOK_SECRET_FILE": "/empty"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadReceiver(func(k string) string { return tt.env[k] }, secretFS)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
