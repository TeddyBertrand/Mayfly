package config

import (
	"reflect"
	"testing"
	"time"
)

// TestLoadProvisioner checks defaults, overrides and rejection of missing or invalid values.
func TestLoadProvisioner(t *testing.T) {
	const testVersion = "0.4.0"
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
				App:    GitHubApp{ID: 5714458, Key: []byte("PEM-BYTES"), APIURL: DefaultGitHubAPIURL},
				Docker: Docker{Socket: DefaultDockerSocket}, WorkerImage: WorkerImageRepo + ":" + testVersion,
				WorkerTimeout: DefaultWorkerTimeout, Workers: 2, QueueSize: 100, PoolSize: 1,
				PoolMax: 2, PoolIdleTimeout: 30 * time.Minute,
			},
		},
		{
			name: "pool max follows pool size",
			env:  with(map[string]string{"MAYFLY_POOL_SIZE": "3"}),
			want: ProvisionerConfig{
				App:    GitHubApp{ID: 5714458, Key: []byte("PEM-BYTES"), APIURL: DefaultGitHubAPIURL},
				Docker: Docker{Socket: DefaultDockerSocket}, WorkerImage: WorkerImageRepo + ":" + testVersion,
				WorkerTimeout: DefaultWorkerTimeout, Workers: 2, QueueSize: 100, PoolSize: 3,
				PoolMax: 6, PoolIdleTimeout: 30 * time.Minute,
			},
		},
		{
			name: "overrides",
			env: with(map[string]string{
				"MAYFLY_GITHUB_API_URL": "http://gh.local", "MAYFLY_DOCKER_SOCKET": "/tmp/d.sock",
				"MAYFLY_WORKER_IMAGE": "img:1", "MAYFLY_WORKER_NETWORK": "mayfly-workers",
				"MAYFLY_WORKER_TIMEOUT": "1h", "MAYFLY_WORKERS": "4", "MAYFLY_QUEUE_SIZE": "10",
				"MAYFLY_POOL_REPOS": "octo/a, octo/b,Octo/A", "MAYFLY_POOL_SIZE": "3",
				"MAYFLY_POOL_MAX": "3", "MAYFLY_POOL_IDLE_TIMEOUT": "10m",
				"MAYFLY_CACHE_URL": "http://cache:3000",
			}),
			want: ProvisionerConfig{
				App:    GitHubApp{ID: 5714458, Key: []byte("PEM-BYTES"), APIURL: "http://gh.local"},
				Docker: Docker{Socket: "/tmp/d.sock"}, WorkerImage: "img:1", WorkerNetwork: "mayfly-workers",
				WorkerTimeout: time.Hour, Workers: 4, QueueSize: 10,
				PoolRepos: []string{"octo/a", "octo/b"}, PoolSize: 3, PoolMax: 3, PoolIdleTimeout: 10 * time.Minute,
				CacheURL: "http://cache:3000/",
			},
		},
		{name: "cache url without scheme", env: with(map[string]string{"MAYFLY_CACHE_URL": "cache:3000"}), wantErr: true},
		{name: "cache url with query", env: with(map[string]string{"MAYFLY_CACHE_URL": "http://cache:3000/?a=1"}), wantErr: true},
		{name: "pool max below size", env: with(map[string]string{"MAYFLY_POOL_SIZE": "3", "MAYFLY_POOL_MAX": "2"}), wantErr: true},
		{name: "bad pool idle timeout", env: with(map[string]string{"MAYFLY_POOL_IDLE_TIMEOUT": "soon"}), wantErr: true},
		{
			name:    "pool idle timeout not below worker timeout",
			env:     with(map[string]string{"MAYFLY_WORKER_TIMEOUT": "20m", "MAYFLY_POOL_IDLE_TIMEOUT": "20m"}),
			wantErr: true,
		},
		{name: "pool repo without owner", env: with(map[string]string{"MAYFLY_POOL_REPOS": "octo/a,b"}), wantErr: true},
		{name: "pool repo empty entry", env: with(map[string]string{"MAYFLY_POOL_REPOS": "octo/a,"}), wantErr: true},
		{name: "pool repo too deep", env: with(map[string]string{"MAYFLY_POOL_REPOS": "octo/a/b"}), wantErr: true},
		{name: "zero pool size", env: with(map[string]string{"MAYFLY_POOL_SIZE": "0"}), wantErr: true},
		{name: "missing app id", env: map[string]string{"MAYFLY_APP_KEY_FILE": "/key.pem"}, wantErr: true},
		{name: "bad app id", env: with(map[string]string{"MAYFLY_APP_ID": "abc"}), wantErr: true},
		{name: "missing key", env: map[string]string{"MAYFLY_APP_ID": "1"}, wantErr: true},
		{name: "zero workers", env: with(map[string]string{"MAYFLY_WORKERS": "0"}), wantErr: true},
		{name: "bad timeout", env: with(map[string]string{"MAYFLY_WORKER_TIMEOUT": "forever"}), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadProvisioner(func(k string) string { return tt.env[k] }, files, testVersion)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
