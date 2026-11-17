package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

func TestSharedSnapshotLoadsOncePerTTLAndKeepsLastGood(t *testing.T) {
	now := time.Unix(1000, 0)
	loads := 0
	degraded := false
	s := &sharedSnapshot{now: func() time.Time { return now }, load: func(ctx context.Context) livedata.Snapshot {
		loads++
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < 10*time.Second {
			t.Fatal("load deadline too tight for a busy VM")
		}
		if degraded {
			return livedata.Snapshot{Degraded: &livedata.SnapDegraded{Reason: "x"}}
		}
		return livedata.Snapshot{OrchestrationMode: "active"}
	}}
	for i := 0; i < 4; i++ {
		s.get()
	}
	if loads != 1 {
		t.Fatalf("four watchers loaded %d times", loads)
	}
	now = now.Add(20 * time.Second)
	degraded = true
	if snap := s.get(); snap.Degraded != nil || loads != 2 {
		t.Fatalf("a failed load replaced the last good one: %+v (%d loads)", snap, loads)
	}
}
