package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/metrics"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

const fakeJIT = "FAKE-JIT-CONFIG"

// fakeEngine is an httptest Docker daemon that records the calls it receives.
type fakeEngine struct {
	mu          sync.Mutex
	calls       []string
	created     containerSpec
	started     chan struct{}
	imageExists bool
	// networkExists and createdNetwork back the worker network endpoints.
	networkExists  bool
	createdNetwork map[string]any
	createCode     int
	startCode      int
	waitForever    bool
	// createFails and startFails make the first N creates or starts answer 503.
	createFails int
	startFails  int
}

// injectFailure reports whether this call should answer 503, counting down *left under the lock.
func (f *fakeEngine) injectFailure(left *int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if *left > 0 {
		*left--
		return true
	}
	return false
}

// newFakeEngine returns a fakeEngine that succeeds on every call by default.
func newFakeEngine() *fakeEngine {
	return &fakeEngine{started: make(chan struct{}), imageExists: true, createCode: http.StatusCreated, startCode: http.StatusNoContent}
}

// record appends a call name, safe for concurrent handlers.
func (f *fakeEngine) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

// callList returns a copy of the recorded calls.
func (f *fakeEngine) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// handler serves the Engine API endpoints the backend uses.
func (f *fakeEngine) handler() http.Handler {
	mux := http.NewServeMux()
	p := "/" + apiVersion
	mux.HandleFunc("GET "+p+"/images/{name...}", func(w http.ResponseWriter, _ *http.Request) {
		f.record("inspect-image")
		if !f.imageExists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST "+p+"/images/create", func(w http.ResponseWriter, _ *http.Request) {
		f.record("pull")
		_, _ = w.Write([]byte(`{"status":"Pulling"}` + "\n"))
	})
	mux.HandleFunc("GET "+p+"/networks/{name}", func(w http.ResponseWriter, _ *http.Request) {
		f.record("inspect-network")
		if !f.networkExists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST "+p+"/networks/create", func(w http.ResponseWriter, r *http.Request) {
		f.record("create-network")
		_ = json.NewDecoder(r.Body).Decode(&f.createdNetwork)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"net1"}`))
	})
	mux.HandleFunc("POST "+p+"/containers/create", func(w http.ResponseWriter, r *http.Request) {
		f.record("create")
		if f.injectFailure(&f.createFails) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		w.WriteHeader(f.createCode)
		if f.createCode == http.StatusCreated {
			_, _ = w.Write([]byte(`{"Id":"abcdef0123456789"}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":"create failed"}`))
	})
	mux.HandleFunc("POST "+p+"/containers/{id}/wait", func(w http.ResponseWriter, r *http.Request) {
		f.record("wait")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if f.waitForever {
			<-r.Context().Done()
			return
		}
		select {
		case <-f.started:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"StatusCode":0}`))
	})
	mux.HandleFunc("POST "+p+"/containers/{id}/start", func(w http.ResponseWriter, _ *http.Request) {
		f.record("start")
		if f.injectFailure(&f.startFails) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(f.startCode)
		if f.startCode == http.StatusNoContent {
			close(f.started)
			return
		}
		_, _ = w.Write([]byte(`{"message":"exec: \"/nope\": no such file"}`))
	})
	mux.HandleFunc("DELETE "+p+"/containers/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.record("remove")
		w.WriteHeader(http.StatusNotFound)
	})
	return mux
}

// newFakeBackend returns a Backend wired to f and a buffer capturing its JSON logs.
func newFakeBackend(t *testing.T, f *fakeEngine, timeout time.Duration) (*Backend, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	b := &Backend{
		eng:     &engine{http: srv.Client(), base: srv.URL + "/" + apiVersion},
		image:   "example/runner:1",
		cmd:     []string{"./run.sh"},
		timeout: timeout,
		logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
		owner:   "instance-a",
		retries: metrics.NewMayfly(nil).Retries.WithLabelValues(metrics.OpWorkerStart),
	}
	return b, &logs
}

// testSpec is the worker spec used across these tests.
var testSpec = worker.Spec{
	JobID: 4242, RunnerName: "mayfly-4242-abcdef", JITConfig: fakeJIT,
	Repo: "octo/demo", InstallationID: 9, RunnerID: 77,
}

// TestRunSequences checks the call order and that every path after create removes the container.
func TestRunSequences(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*fakeEngine)
		timeout   time.Duration
		wantErr   bool
		wantCalls []string
		wantLog   string
	}{
		{
			name:      "success",
			setup:     func(*fakeEngine) {},
			wantCalls: []string{"inspect-image", "create", "wait", "start", "remove"},
			wantLog:   "worker finished",
		},
		{
			name:      "image missing is pulled",
			setup:     func(f *fakeEngine) { f.imageExists = false },
			wantCalls: []string{"inspect-image", "pull", "create", "wait", "start", "remove"},
			wantLog:   "worker finished",
		},
		{
			name:      "create fails",
			setup:     func(f *fakeEngine) { f.createCode = http.StatusInternalServerError },
			wantErr:   true,
			wantCalls: []string{"inspect-image", "create"},
			wantLog:   "worker create failed",
		},
		{
			name:      "start fails",
			setup:     func(f *fakeEngine) { f.startCode = http.StatusBadRequest },
			wantErr:   true,
			wantCalls: []string{"inspect-image", "create", "wait", "start", "remove"},
			wantLog:   "worker start failed",
		},
		{
			name:      "timeout",
			setup:     func(f *fakeEngine) { f.waitForever = true },
			timeout:   100 * time.Millisecond,
			wantErr:   true,
			wantCalls: []string{"inspect-image", "create", "wait", "start", "remove"},
			wantLog:   "worker timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeEngine()
			tt.setup(f)
			timeout := tt.timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}
			b, logs := newFakeBackend(t, f, timeout)

			err := b.Run(context.Background(), testSpec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got := f.callList(); !reflect.DeepEqual(got, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", got, tt.wantCalls)
			}
			out := logs.String()
			if !strings.Contains(out, tt.wantLog) || !strings.Contains(out, `"job_id":4242`) {
				t.Errorf("logs missing %q with job_id:\n%s", tt.wantLog, out)
			}
			if strings.Contains(out, fakeJIT) {
				t.Errorf("logs leak the JIT config:\n%s", out)
			}
		})
	}
}

// TestRunCancelledLogsNoError checks stopping a running worker removes its container without logging an error.
func TestRunCancelledLogsNoError(t *testing.T) {
	f := newFakeEngine()
	f.waitForever = true
	b, logs := newFakeBackend(t, f, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-f.started
		// Lets start's response reach the backend, so the cancel lands on the wait.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := b.Run(ctx, testSpec); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if got := f.callList(); got[len(got)-1] != "remove" {
		t.Errorf("calls = %v, want a final remove", got)
	}
	out := logs.String()
	if !strings.Contains(out, "worker stopped before it exited") || strings.Contains(out, `"level":"ERROR"`) {
		t.Errorf("want an info stop and no error:\n%s", out)
	}
}

// TestContainerSpec checks image, command, JIT env, labels and AutoRemove in the create request.
func TestContainerSpec(t *testing.T) {
	f := newFakeEngine()
	b, _ := newFakeBackend(t, f, 5*time.Second)
	if err := b.Run(context.Background(), testSpec); err != nil {
		t.Fatal(err)
	}

	got := f.created
	if got.Image != "example/runner:1" || !reflect.DeepEqual(got.Cmd, []string{"./run.sh"}) {
		t.Errorf("image/cmd = %q %v", got.Image, got.Cmd)
	}
	if !reflect.DeepEqual(got.Env, []string{"ACTIONS_RUNNER_INPUT_JITCONFIG=" + fakeJIT}) {
		t.Errorf("env = %v", got.Env)
	}
	wantLabels := map[string]string{
		LabelJobID: "4242", LabelRunnerName: "mayfly-4242-abcdef", LabelOwner: "instance-a",
		LabelRepo: "octo/demo", LabelInstallationID: "9", LabelRunnerID: "77",
	}
	if !reflect.DeepEqual(got.Labels, wantLabels) {
		t.Errorf("labels = %v, want %v", got.Labels, wantLabels)
	}
	if !got.HostConfig.AutoRemove {
		t.Error("AutoRemove not set")
	}
	if got.HostConfig.NetworkMode != "" {
		t.Errorf("NetworkMode = %q, want empty (Docker default)", got.HostConfig.NetworkMode)
	}

	b.network = "mayfly-workers"
	if got := b.containerSpec(testSpec).HostConfig.NetworkMode; got != "mayfly-workers" {
		t.Errorf("NetworkMode = %q, want mayfly-workers", got)
	}

	b.cacheURL = "http://cache:3000/"
	wantEnv := []string{"ACTIONS_RUNNER_INPUT_JITCONFIG=" + fakeJIT, "ACTIONS_RESULTS_URL=http://cache:3000/"}
	if got := b.containerSpec(testSpec).Env; !reflect.DeepEqual(got, wantEnv) {
		t.Errorf("env = %v, want %v", got, wantEnv)
	}

	b.proxyEnv = proxyEnv(Options{GoProxyURL: "http://goproxy:3000/", NPMRegistryURL: "http://npm-proxy:4873/"})
	wantEnv = append(wantEnv, "GOPROXY=http://goproxy:3000/", "npm_config_registry=http://npm-proxy:4873/")
	if got := b.containerSpec(testSpec).Env; !reflect.DeepEqual(got, wantEnv) {
		t.Errorf("env = %v, want %v", got, wantEnv)
	}
}

// TestWorkerNetwork checks that a configured network is created once when missing, with a named bridge.
func TestWorkerNetwork(t *testing.T) {
	f := newFakeEngine()
	b, _ := newFakeBackend(t, f, 5*time.Second)
	b.network = "mayfly-workers"
	if err := b.Run(context.Background(), testSpec); err != nil {
		t.Fatal(err)
	}
	want := []string{"inspect-image", "inspect-network", "create-network", "create", "wait", "start", "remove"}
	if got := f.callList(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	opts, _ := f.createdNetwork["Options"].(map[string]any)
	if f.createdNetwork["Name"] != "mayfly-workers" || opts["com.docker.network.bridge.name"] != "mayfly-workers" {
		t.Errorf("network create body = %v", f.createdNetwork)
	}
	if f.created.HostConfig.NetworkMode != "mayfly-workers" {
		t.Errorf("NetworkMode = %q", f.created.HostConfig.NetworkMode)
	}
}

// TestSpecRedacted checks that a worker.Spec never prints its JIT config.
func TestSpecRedacted(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "spec", testSpec)
	for _, out := range []string{buf.String(), testSpec.String()} {
		if strings.Contains(out, fakeJIT) {
			t.Errorf("spec leaks JIT config: %s", out)
		}
	}
}
