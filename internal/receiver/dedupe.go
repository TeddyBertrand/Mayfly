package receiver

import (
	"sync"
	"time"
)

// Deduper remembers delivery IDs so a repeated delivery is processed once.
type Deduper interface {
	// Seen reports whether id was already recorded, and records it if not, in one atomic step.
	Seen(id string) bool
}

// MemoryDeduper is an in-process Deduper whose entries expire after a TTL.
type MemoryDeduper struct {
	mu        sync.Mutex
	ttl       time.Duration
	now       func() time.Time
	seen      map[string]time.Time
	lastSweep time.Time
}

// NewMemoryDeduper returns a MemoryDeduper that forgets IDs after ttl.
func NewMemoryDeduper(ttl time.Duration) *MemoryDeduper {
	return &MemoryDeduper{ttl: ttl, now: time.Now, seen: make(map[string]time.Time)}
}

// Seen implements Deduper.
func (d *MemoryDeduper) Seen(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	if now.Sub(d.lastSweep) >= d.ttl {
		d.sweep(now)
	}
	if at, ok := d.seen[id]; ok && now.Sub(at) < d.ttl {
		return true
	}
	d.seen[id] = now
	return false
}

// sweep drops expired entries so memory stays bounded; the caller holds d.mu.
func (d *MemoryDeduper) sweep(now time.Time) {
	for id, at := range d.seen {
		if now.Sub(at) >= d.ttl {
			delete(d.seen, id)
		}
	}
	d.lastSweep = now
}

// len returns the number of stored IDs, for tests.
func (d *MemoryDeduper) len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}
