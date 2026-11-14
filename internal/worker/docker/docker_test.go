package docker

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/config"
	"github.com/TeddyBertrand/Mayfly/internal/worker"
)

const smallImage = "busybox:1.36"

// realBackend returns a Backend on the local Docker daemon, or skips the test when none answers.
func realBackend(t *testing.T, image string, cmd []string, timeout time.Duration) (*Backend, *bytes.Buffer) {
	t.Helper()
	b := New(config.DefaultDockerSocket, image, "", timeout, "test-instance", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.eng.call(ctx, "ping", "GET", "/_ping", nil, nil, 200); err != nil {
		t.Skipf("no Docker daemon at %s: %v", config.DefaultDockerSocket, err)
	}
	var logs bytes.Buffer
	b.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if cmd != nil {
		b.cmd = cmd
	}
	return b, &logs
}

// assertGone fails the test if a container called name still exists.
func assertGone(t *testing.T, b *Backend, name string) {
	t.Helper()
	exists, err := b.eng.containerExists(context.Background(), name)
	if err != nil {
		t.Fatalf("inspect %s: %v", name, err)
	}
	if exists {
		t.Errorf("container %s still exists", name)
		_ = b.eng.removeContainer(context.Background(), name)
	}
}

// uniqueSpec returns a Spec with a runner name unique to this test run.
func uniqueSpec(t *testing.T) worker.Spec {
	id := time.Now().UnixNano()
	name := "mayfly-test-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + time.Now().Format("150405.000000")
	return worker.Spec{JobID: id, RunnerName: strings.ReplaceAll(name, ".", ""), JITConfig: fakeJIT}
}

// TestDockerContainerGoneAfterExit runs a container that exits by itself and checks it no longer exists.
func TestDockerContainerGoneAfterExit(t *testing.T) {
	b, _ := realBackend(t, smallImage, []string{"sh", "-c", "sleep 1"}, time.Minute)
	spec := uniqueSpec(t)
	if err := b.Run(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	assertGone(t, b, spec.RunnerName)
}

// TestDockerStartFailureLoggedAndRemoved uses a missing binary so the daemon refuses to start the container.
func TestDockerStartFailureLoggedAndRemoved(t *testing.T) {
	b, logs := realBackend(t, smallImage, []string{"/does-not-exist"}, time.Minute)
	spec := uniqueSpec(t)
	if err := b.Run(context.Background(), spec); err == nil {
		t.Fatal("expected a start error")
	}
	out := logs.String()
	if !strings.Contains(out, "worker start failed") || !strings.Contains(out, `"job_id":`) {
		t.Errorf("start failure not logged with job_id:\n%s", out)
	}
	assertGone(t, b, spec.RunnerName)
}

// TestDockerTimeoutRemovesContainer cuts a long job short and checks the container is gone.
func TestDockerTimeoutRemovesContainer(t *testing.T) {
	b, _ := realBackend(t, smallImage, []string{"sleep", "60"}, 2*time.Second)
	spec := uniqueSpec(t)
	if err := b.Run(context.Background(), spec); err == nil {
		t.Fatal("expected a timeout error")
	}
	assertGone(t, b, spec.RunnerName)
}

// TestDockerRunnerImage runs the real runner image with a fake JIT config; it pulls ~500 MB,
// so it only runs with MAYFLY_DOCKER_TEST=1.
func TestDockerRunnerImage(t *testing.T) {
	if os.Getenv("MAYFLY_DOCKER_TEST") != "1" {
		t.Skip("set MAYFLY_DOCKER_TEST=1 to run the real runner image")
	}
	b, logs := realBackend(t, config.DefaultWorkerImage, nil, 5*time.Minute)
	spec := uniqueSpec(t)
	if err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v\n%s", err, logs.String())
	}
	assertGone(t, b, spec.RunnerName)
}
