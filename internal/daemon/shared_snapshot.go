package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// The watchers (op watch, invariant, pitch, Commander check-in) each used to
// load the whole board on their own tick with a 3-second deadline. On a busy
// VM a load of the largest project takes 3-7 seconds, so most of those loads
// failed ("live state read failed") and each one added more load. They now
// share one load: a fresh one at most every sharedSnapshotTTL, one at a time,
// with a deadline a real load fits in.
const (
	sharedSnapshotTTL     = 15 * time.Second
	sharedSnapshotTimeout = 30 * time.Second
)

type sharedSnapshot struct {
	mu   sync.Mutex
	at   time.Time
	snap livedata.Snapshot
	load func(ctx context.Context) livedata.Snapshot
	now  func() time.Time
}

// get returns the shared snapshot, loading it when it is older than the TTL.
// A failed load keeps the last good one for up to a minute.
func (s *sharedSnapshot) get() livedata.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !s.at.IsZero() && now.Sub(s.at) < sharedSnapshotTTL {
		return s.snap
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedSnapshotTimeout)
	defer cancel()
	snap := s.load(ctx)
	if snap.Degraded != nil && !s.at.IsZero() && now.Sub(s.at) < time.Minute {
		return s.snap
	}
	s.at, s.snap = now, snap
	return snap
}
