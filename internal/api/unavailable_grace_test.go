package api

import (
	"context"
	"testing"
	"time"

	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/pgtest"
)

// Nov 17: reads hit their deadline under load and the app showed "Current
// database state is unavailable" with an empty board. A failing read keeps the
// last good board on screen, marked stale, for as long as it fails.
func TestFailingLiveStateReadsKeepTheLastGoodBoardMarkedStale(t *testing.T) {
	st, _ := pgtest.Open(t)
	healthy := true
	s := New(Options{Token: "t", StateStore: st, Source: func(context.Context) livedata.Snapshot {
		if healthy {
			return livedata.Snapshot{Storage: "postgres", Workstreams: []livedata.SnapWorkstream{{BeadID: "a"}}}
		}
		return livedata.Snapshot{Storage: "postgres", Degraded: &livedata.SnapDegraded{Reason: "timeout"}}
	}})
	ctx := context.Background()
	shown := func() livedata.Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return s.cur }
	s.Refresh(ctx)
	healthy = false
	s.Refresh(ctx)
	if c := shown(); c.Degraded != nil || len(c.Workstreams) != 1 {
		t.Fatalf("one failed read reported unavailable: %#v", c)
	}
	good := shown().LoadCompletedAt
	s.failingSince = time.Now().Add(-time.Hour) // an hour of failing reads (deadline exceeded)
	s.Refresh(ctx)
	if c := shown(); c.Degraded != nil || len(c.Workstreams) != 1 || !c.LoadError || !c.LoadCompletedAt.Equal(good) {
		t.Fatalf("reads failing past the grace blanked or unmarked the board: degraded=%v rows=%d load_error=%v completed=%v (good %v)",
			c.Degraded, len(c.Workstreams), c.LoadError, c.LoadCompletedAt, good)
	}
	healthy = true
	s.Refresh(ctx)
	if c := shown(); c.Degraded != nil {
		t.Fatalf("recovery: %#v", c)
	}
}

func TestFailedLiveStateReadWithNoGoodFrameIsUnavailableAtOnce(t *testing.T) {
	st, _ := pgtest.Open(t)
	s := New(Options{Token: "t", StateStore: st, Source: func(context.Context) livedata.Snapshot {
		return livedata.Snapshot{Storage: "postgres", Degraded: &livedata.SnapDegraded{Reason: "timeout"}}
	}})
	s.Refresh(context.Background())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.Degraded == nil {
		t.Fatal("no good frame to keep, but unavailable was not reported")
	}
}

func TestBoardSkipsChangesItNeverReads(t *testing.T) {
	for _, c := range []struct {
		entities []string
		want     bool
	}{
		{nil, true},
		{[]string{"usage_turns"}, false},
		{[]string{"usage_turns", "bead_supervision", "supervision_attempts"}, false},
		{[]string{"usage_turns", "bead_tasks"}, true},
		{[]string{"agent_state"}, true},
	} {
		if got := boardReadsAny(c.entities); got != c.want {
			t.Errorf("%v: %v, want %v", c.entities, got, c.want)
		}
	}
}
