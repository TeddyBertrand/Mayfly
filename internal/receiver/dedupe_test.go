package receiver

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestDeduper returns a MemoryDeduper driven by a manual clock and a function to advance it.
func newTestDeduper(ttl time.Duration) (*MemoryDeduper, func(time.Duration)) {
	now := time.Unix(0, 0)
	d := NewMemoryDeduper(ttl)
	d.now = func() time.Time { return now }
	return d, func(step time.Duration) { now = now.Add(step) }
}

// TestMemoryDeduperRepeatAndExpiry checks first-seen, repeat, distinct IDs and TTL expiry.
func TestMemoryDeduperRepeatAndExpiry(t *testing.T) {
	d, advance := newTestDeduper(time.Hour)

	if d.Seen("a") {
		t.Fatal("first a reported as seen")
	}
	if !d.Seen("a") {
		t.Fatal("repeat a not reported as seen")
	}
	if d.Seen("b") {
		t.Fatal("distinct b reported as seen")
	}

	advance(time.Hour)
	if d.Seen("a") {
		t.Fatal("a still seen after ttl")
	}
	if got := d.len(); got != 1 {
		t.Fatalf("stored ids = %d after sweep, want 1", got)
	}
}

// TestMemoryDeduperConcurrent checks that concurrent calls with one ID let exactly one through.
func TestMemoryDeduperConcurrent(t *testing.T) {
	d := NewMemoryDeduper(time.Hour)
	var fresh atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !d.Seen("same") {
				fresh.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := fresh.Load(); got != 1 {
		t.Fatalf("fresh = %d, want 1", got)
	}
}
