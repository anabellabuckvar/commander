package daemon

import (
	"testing"
	"time"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
)

// The Nov 16 shape: #4 Chart open's fix A (vibe-apps-zq9h.6.63.14) approved and
// held only by "Manager dispatch", its blocker done, no Manager chat running.
func TestAutoDispatchPicksApprovedWorkNoManagerWillStart(t *testing.T) {
	now := time.Now()
	receipt := beads.Wait{Kind: beads.WaitUntil, Since: now.Add(-time.Hour), Until: now.Add(-58 * time.Minute), Ref: beads.ManagerDispatchWait}
	step := func(id string, f func(*livedata.SnapStep)) livedata.SnapStep {
		s := livedata.SnapStep{BeadID: id, Title: id, Status: "open", State: "not-started", ExecutionMode: "active", TypedWaits: []beads.Wait{receipt}}
		if f != nil {
			f(&s)
		}
		return s
	}
	snap := livedata.Snapshot{
		Agents: map[string]livedata.SnapAgent{"manager-vibe-apps-zq9h.6": {State: store.AgentIdle, Detail: "Process is not running"}},
		Workstreams: []livedata.SnapWorkstream{{ProjectID: 2, Project: "vibe-apps", BeadID: "vibe-apps-zq9h.6", ManagerRole: "manager-vibe-apps-zq9h.6", Steps: []livedata.SnapStep{
			{BeadID: "vibe-apps-zq9h.6.63", Title: "Chart open", Status: "open", Track: true, Children: []livedata.SnapStep{
				step("vibe-apps-zq9h.6.63.13", func(s *livedata.SnapStep) { s.Status, s.State, s.TypedWaits = "closed", "done", nil }),
				step("vibe-apps-zq9h.6.63.14", func(s *livedata.SnapStep) { s.BlockedBy = []string{"vibe-apps-zq9h.6.63.13"} }),
				step("blocked", func(s *livedata.SnapStep) { s.BlockedBy = []string{"vibe-apps-zq9h.6.63.99"} }),
				step("vibe-apps-zq9h.6.63.99", func(s *livedata.SnapStep) { s.TypedWaits = []beads.Wait{{Kind: beads.WaitCI, PR: 3772}} }),
				step("gate", func(s *livedata.SnapStep) { s.Gate = true }),
				step("result", func(s *livedata.SnapStep) { s.ResultReadyAt = now.Unix() }),
				step("planning", func(s *livedata.SnapStep) { s.PlanningPending = true }),
				step("held", func(s *livedata.SnapStep) { s.Hold = true }),
				step("ben", func(s *livedata.SnapStep) { s.Lane = "elsewhere:Ben" }),
				step("running", nil),
				step("vibe-apps-zq9h.6.66.24", nil),
			}},
		}}},
		Executions: []livedata.SnapExecution{{ProjectID: 2, BeadID: "running", Role: "operator-x"}},
	}
	got := map[string]bool{}
	for _, j := range autoDispatchJobs(snap) {
		got[j.bead] = true
	}
	// A step in the Commander's custody (planning pending) is still started:
	// the Commander is never on the critical path.
	if len(got) != 3 || !got["vibe-apps-zq9h.6.63.14"] || !got["vibe-apps-zq9h.6.66.24"] || !got["planning"] {
		t.Fatalf("dispatch jobs = %v, want the three unblocked, unclaimed agent steps", got)
	}
	// The system starts an agent on every unblocked step: a running Manager
	// (a Commander) is never on the critical path.
	snap.Agents["manager-vibe-apps-zq9h.6"] = livedata.SnapAgent{State: store.AgentIdle}
	if jobs := autoDispatchJobs(snap); len(jobs) != 3 {
		t.Fatalf("with its Manager running cmdrd dispatched %v, want the same two steps", jobs)
	}
}
