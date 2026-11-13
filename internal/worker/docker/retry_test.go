package docker

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TeddyBertrand/Mayfly/internal/retry"
)

// TestRunRetriesStart checks that daemon 5xx answers during setup are retried, each failed attempt is
// cleaned up, and the job wait itself is never retried.
func TestRunRetriesStart(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*fakeEngine)
		wantErr   string
		wantCalls []string
	}{
		{
			name:      "create recovers",
			setup:     func(f *fakeEngine) { f.createFails = 2 },
			wantCalls: []string{"inspect-image", "create", "inspect-image", "create", "inspect-image", "create", "wait", "start", "remove"},
		},
		{
			name:      "start recovers, failed container removed",
			setup:     func(f *fakeEngine) { f.startFails = 1 },
			wantCalls: []string{"inspect-image", "create", "wait", "start", "remove", "inspect-image", "create", "wait", "start", "remove"},
		},
		{
			name:      "gives up",
			setup:     func(f *fakeEngine) { f.createFails = 10 },
			wantErr:   "gave up after 3 attempts: docker create: 503",
			wantCalls: []string{"inspect-image", "create", "inspect-image", "create", "inspect-image", "create"},
		},
		{
			name:      "client error not retried",
			setup:     func(f *fakeEngine) { f.startCode = http.StatusBadRequest },
			wantErr:   "attempt 1 of 3, not retryable: docker start: 400",
			wantCalls: []string{"inspect-image", "create", "wait", "start", "remove"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeEngine()
			tt.setup(f)
			b, logs := newFakeBackend(t, f, 5*time.Second)
			b.retry = retry.Policy{Attempts: 3, Base: time.Millisecond, Max: 5 * time.Millisecond}

			err := b.Run(context.Background(), testSpec)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("err = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if got := f.callList(); !reflect.DeepEqual(got, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", got, tt.wantCalls)
			}
			if strings.Contains(logs.String(), fakeJIT) {
				t.Errorf("logs leak the JIT config:\n%s", logs.String())
			}
		})
	}
}
