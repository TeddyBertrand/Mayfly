// Package retry runs an operation again after temporary failures, with exponential backoff and jitter.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Policy says how many times to try and how long to wait between attempts.
type Policy struct {
	Attempts int
	Base     time.Duration
	Max      time.Duration
	// jitter returns a random value in [0, n]; nil uses math/rand. Tests replace it.
	jitter func(n int64) int64
}

// Default is the policy for GitHub and Docker calls: 5 attempts, waits capped at 1s, 2s, 4s, 8s.
var Default = Policy{Attempts: 5, Base: time.Second, Max: 30 * time.Second}

// Notify is called before each wait with the attempt that just failed, the wait, and its error.
type Notify func(attempt int, wait time.Duration, err error)

// Do calls fn until it succeeds, returns an error that is not temporary, runs out of attempts,
// or ctx ends. The returned error wraps the last error from fn and says why it stopped.
func Do(ctx context.Context, p Policy, fn func(context.Context) error, notify Notify) error {
	for attempt := 1; ; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if !Temporary(err) {
			return fmt.Errorf("attempt %d of %d, not retryable: %w", attempt, p.Attempts, err)
		}
		if attempt >= p.Attempts {
			return fmt.Errorf("gave up after %d attempts: %w", attempt, err)
		}

		wait := p.backoff(attempt)
		if after := After(err); after > 0 {
			if after > p.Max {
				return fmt.Errorf("server asked to wait %s, more than %s, after %d attempts: %w", after, p.Max, attempt, err)
			}
			// Jitter on top of the server's delay so callers told the same time do not all come back at once.
			wait = after + time.Duration(p.rand(int64(p.Base)))
		}
		if notify != nil {
			notify(attempt, wait, err)
		}

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("stopped after %d attempts (%w): %w", attempt, ctx.Err(), err)
		case <-t.C:
		}
	}
}

// backoff returns a random wait in [0, min(Max, Base*2^(attempt-1))], the "full jitter" strategy.
func (p Policy) backoff(attempt int) time.Duration {
	limit := p.Base
	for i := 1; i < attempt && limit < p.Max; i++ {
		limit *= 2
	}
	limit = min(limit, p.Max)
	return time.Duration(p.rand(int64(limit)))
}

// rand returns a random value in [0, n].
func (p Policy) rand(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if p.jitter != nil {
		return p.jitter(n)
	}
	return rand.Int64N(n + 1)
}

// Temporary reports whether err, or an error it wraps, says it is worth another attempt.
// Errors that do not say so are treated as permanent.
func Temporary(err error) bool {
	var t interface{ Temporary() bool }
	return errors.As(err, &t) && t.Temporary()
}

// After returns the wait a server asked for in err, or zero if none was given.
func After(err error) time.Duration {
	var a interface{ RetryAfter() time.Duration }
	if errors.As(err, &a) {
		return a.RetryAfter()
	}
	return 0
}
