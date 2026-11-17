package daemon

import (
	"testing"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/livedata"
)

// The watcher: agent-owned, not done, unblocked, no live agent: start one.
func TestWatcherStartsExactlyWhatHasNoLiveAgent(t *testing.T) {
	done := livedata.SnapStep{BeadID: "d", Status: "closed"}
	byID := map[string]livedata.SnapStep{"d": done, "o": {BeadID: "o", Status: "open"}}
	base := livedata.SnapStep{BeadID: "s", Title: "Build it", Status: "open", BlockedBy: []string{"d"}}
	cases := []struct {
		name string
		f    func(*livedata.SnapStep)
		want bool
	}{
		{"unblocked agent step with no brief", func(s *livedata.SnapStep) {}, true},
		{"a review step", func(s *livedata.SnapStep) { s.Lane = "agents"; s.Title = "Review: the PR" }, true},
		{"its agent died", func(s *livedata.SnapStep) {
			s.ResultReadyAt, s.ResultLine = 5, "Execution exited without a recorded final report."
		}, true},
		{"in progress, agent stopped", func(s *livedata.SnapStep) { s.Status = "in_progress"; s.OwnerState = livedata.OwnerDied }, true},
		{"a live agent runs it", func(s *livedata.SnapStep) { s.LiveRuntime = "codex" }, false},
		{"queued for an agent", func(s *livedata.SnapStep) { s.ExecutionReady = true }, false},
		{"asleep on its event", func(s *livedata.SnapStep) { s.TypedWaits = []beads.Wait{{Kind: beads.WaitDeploy, Ref: "abc"}} }, false},
		{"blocked", func(s *livedata.SnapStep) { s.BlockedBy = []string{"o"} }, false},
		{"finished, closing itself", func(s *livedata.SnapStep) { s.ResultReadyAt, s.ResultLine = 5, "Final report: done" }, false},
		{"Eric's step", func(s *livedata.SnapStep) { s.Lane = "you" }, false},
		{"a gate", func(s *livedata.SnapStep) { s.Gate = true }, false},
		{"on Eric's list", func(s *livedata.SnapStep) { s.State = "needs-user" }, false},
	}
	for _, c := range cases {
		s := base
		c.f(&s)
		if got := dispatchable(s, byID); got != c.want {
			t.Errorf("%s: dispatchable = %v, want %v", c.name, got, c.want)
		}
	}
}
