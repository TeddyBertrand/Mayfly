package retry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// tempErr is an injected error that says whether to retry and how long the "server" asked to wait.
type tempErr struct {
	temp  bool
	after time.Duration
}

// Error implements error.
func (e tempErr) Error() string { return "injected" }

// Temporary reports whether the injected error should be retried.
func (e tempErr) Temporary() bool { return e.temp }

// RetryAfter returns the injected server-requested wait.
func (e tempErr) RetryAfter() time.Duration { return e.after }

// fast returns a policy with tiny waits and no randomness, so tests are quick and repeatable.
func fast(attempts int) Policy {
	return Policy{Attempts: attempts, Base: time.Millisecond, Max: 10 * time.Millisecond, jitter: func(n int64) int64 { return n }}
}

// TestDo checks success, recovery, giving up, permanent errors and Retry-After handling.
func TestDo(t *testing.T) {
	tests := []struct {
		name      string
		policy    Policy
		errs      []error
		wantCalls int
		wantErr   string
	}{
		{"success first try", fast(3), nil, 1, ""},
		{"recovers after failures", fast(3), []error{tempErr{temp: true}, tempErr{temp: true}}, 3, ""},
		{"gives up", fast(3), []error{tempErr{temp: true}, tempErr{temp: true}, tempErr{temp: true}}, 3, "gave up after 3 attempts: injected"},
		{"permanent error stops", fast(3), []error{tempErr{temp: false}}, 1, "attempt 1 of 3, not retryable: injected"},
		{"plain error is permanent", fast(3), []error{errors.New("boom")}, 1, "not retryable: boom"},
		{"retry-after honoured", fast(3), []error{tempErr{temp: true, after: 2 * time.Millisecond}}, 2, ""},
		{"retry-after too long", fast(3), []error{tempErr{temp: true, after: time.Hour}}, 1, "server asked to wait 1h0m0s, more than 10ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, notified := 0, 0
			err := Do(context.Background(), tt.policy, func(context.Context) error {
				calls++
				if calls <= len(tt.errs) {
					return tt.errs[calls-1]
				}
				return nil
			}, func(int, time.Duration, error) { notified++ })

			if calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tt.wantCalls)
			}
			if notified != calls-1 && tt.wantErr == "" {
				t.Errorf("notified = %d, want %d", notified, calls-1)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("err = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, tt.errs[calls-1]) {
				t.Errorf("err does not wrap the last error from fn: %v", err)
			}
		})
	}
}

// TestDoStopsOnContextCancel checks that a cancelled context ends a wait at once and is reported.
func TestDoStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := Policy{Attempts: 5, Base: time.Hour, Max: time.Hour, jitter: func(n int64) int64 { return n }}
	err := Do(ctx, p, func(context.Context) error { return tempErr{temp: true} },
		func(int, time.Duration, error) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "stopped after 1 attempts") {
		t.Errorf("err = %v, want attempt count", err)
	}
}

// TestBackoffLimits checks the wait limit doubles per attempt and stops at Max.
func TestBackoffLimits(t *testing.T) {
	p := Policy{Base: time.Second, Max: 5 * time.Second, jitter: func(n int64) int64 { return n }}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, w := range want {
		if got := p.backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %s, want %s", i+1, got, w)
		}
	}
}

// TestBackoffJitterInRange checks real random waits stay inside [0, limit].
func TestBackoffJitterInRange(t *testing.T) {
	p := Policy{Base: time.Millisecond, Max: 8 * time.Millisecond}
	for range 1000 {
		if got := p.backoff(4); got < 0 || got > 8*time.Millisecond {
			t.Fatalf("backoff(4) = %s, outside [0, 8ms]", got)
		}
	}
}
