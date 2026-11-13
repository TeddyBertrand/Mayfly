package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/github"
	"github.com/TeddyBertrand/Mayfly/internal/worker/docker"
)

// TestLoadProvisioner checks defaults, overrides and rejection of missing or invalid values.
func TestLoadProvisioner(t *testing.T) {
	files := fakeFS(map[string]string{"/key.pem": "PEM-BYTES\n"})
	base := map[string]string{"MAYFLY_APP_ID": "5714458", "MAYFLY_APP_KEY_FILE": "/key.pem"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	tests := []struct {
		name    string
		env     map[string]string
		want    ProvisionerConfig
		wantErr bool
	}{
		{
			name: "defaults",
			env:  base,
			want: ProvisionerConfig{
				AppID: 5714458, AppKey: []byte("PEM-BYTES"), GitHubAPIURL: github.DefaultBaseURL,
				DockerSocket: docker.DefaultSocket, WorkerImage: docker.DefaultImage,
				WorkerTimeout: docker.DefaultTimeout, Workers: 2, QueueSize: 100,
			},
		},
		{
			name: "overrides",
			env: with(map[string]string{
				"MAYFLY_GITHUB_API_URL": "http://gh.local", "MAYFLY_DOCKER_SOCKET": "/tmp/d.sock",
				"MAYFLY_WORKER_IMAGE": "img:1", "MAYFLY_WORKER_NETWORK": "mayfly-workers",
				"MAYFLY_WORKER_TIMEOUT": "1h", "MAYFLY_WORKERS": "4", "MAYFLY_QUEUE_SIZE": "10",
			}),
			want: ProvisionerConfig{
				AppID: 5714458, AppKey: []byte("PEM-BYTES"), GitHubAPIURL: "http://gh.local",
				DockerSocket: "/tmp/d.sock", WorkerImage: "img:1", WorkerNetwork: "mayfly-workers",
				WorkerTimeout: time.Hour, Workers: 4, QueueSize: 10,
			},
		},
		{name: "missing app id", env: map[string]string{"MAYFLY_APP_KEY_FILE": "/key.pem"}, wantErr: true},
		{name: "bad app id", env: with(map[string]string{"MAYFLY_APP_ID": "abc"}), wantErr: true},
		{name: "missing key", env: map[string]string{"MAYFLY_APP_ID": "1"}, wantErr: true},
		{name: "zero workers", env: with(map[string]string{"MAYFLY_WORKERS": "0"}), wantErr: true},
		{name: "bad timeout", env: with(map[string]string{"MAYFLY_WORKER_TIMEOUT": "forever"}), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadProvisioner(func(k string) string { return tt.env[k] }, files)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
