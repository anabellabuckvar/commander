package daemon

import (
	"context"
	"slices"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
)

const autoDispatchEvery = 30 * time.Second

// dispatchJob is approved work waiting on a Manager that is not running.
type dispatchJob struct {
	projectID int64
	project   string
	bead      string
	title     string
	died      bool // its last agent died: restarted unless maxDeaths died in a row
}

// maxDeaths agents dying in a row without a report turn the step into a card.
const maxDeaths = 3

// deathCard puts a step whose agents keep dying on the user's list.
func deathCard(ctx context.Context, j dispatchJob, n int, run func(ctx context.Context, args ...string) error) {
	decision := fmt.Sprintf(`{"question":"The agent on %q stopped %d times in a row without a report. Try again, or drop the step?","options":[{"label":"Try again","recommended":true},{"label":"Drop it"}]}`, j.title, n)
	if err := run(ctx, "internal-set-objective", "--project", j.project, "--bead", j.bead, "--state", "needs-user", "--ask", "Agent keeps stopping", "--decision", decision, "--no-notify"); err != nil {
		log.Printf("auto-dispatch: %s %s: card after %d deaths: %v", j.project, j.bead, n, err)
		return
	}
	log.Printf("auto-dispatch: %s %s: %d agents died in a row; the step is on the user's list", j.project, j.bead, n)
}

// autoDispatchJobs are the steps whose only holder is a Manager dispatch (or
// start) receipt, ready to run now, under a workstream with no Manager
// running: dispatching them is mechanical, so cmdrd does it instead of
// waiting for a Manager that will never come or for the Commander's turn.
// Anything needing judgment (a gate, a card, a result, a stop, a plan still
// pending, another wait, unfinished blockers or children) is left alone.
func autoDispatchJobs(snap livedata.Snapshot) []dispatchJob {
	type key struct {
		project int64
		bead    string
	}
	running := map[key]bool{} // beads with a live execution
	for _, x := range snap.Executions {
		running[key{x.ProjectID, x.BeadID}] = true
		running[key{x.ProjectID, x.TaskBeadID}] = true
	}
	asked := map[string]bool{}
	for _, n := range snap.NeedsYou {
		asked[n.Project+"\x00"+n.BeadID] = true
	}
	var out []dispatchJob
	for _, ws := range snap.Workstreams {
		byID := map[string]livedata.SnapStep{}
		var index func([]livedata.SnapStep)
		index = func(steps []livedata.SnapStep) {
			for _, s := range steps {
				byID[s.BeadID] = s
				index(s.Children)
			}
		}
		index(ws.Steps)
		var walk func([]livedata.SnapStep)
		walk = func(steps []livedata.SnapStep) {
			for _, s := range steps {
				// Nothing under a paused, held or closed stage or track starts.
				if s.Hold || s.Status == "deferred" || s.Status == "closed" || s.Superseded || s.Withdrawn {
					continue
				}
				if dispatchable(s, byID) && !running[key{ws.ProjectID, s.BeadID}] && !asked[ws.Project+"\x00"+s.BeadID] {
					out = append(out, dispatchJob{projectID: ws.ProjectID, project: ws.Project, bead: s.BeadID, title: s.Title, died: agentDied(s)})
				}
				walk(s.Children)
			}
		}
		if ws.Status != "deferred" && !ws.Hold {
			walk(ws.Steps)
		}
	}
	return out
}

// The watcher: every step that is agent-owned, not done and unblocked, with
// no live agent on it, gets one. That is the only way agents start. A step
// has at most one live agent: a step with a live, starting or queued agent is
// skipped, never given a second.
func dispatchable(s livedata.SnapStep, byID map[string]livedata.SnapStep) bool {
	return agentOwned(s) && !stepDone(s) && !stepBlocked(s, byID) && !liveAgent(s)
}

// agentOwned: an implementer's or a reviewer's step, not the user's, someone
// outside's, a gate or a container.
func agentOwned(s livedata.SnapStep) bool {
	return !s.Gate && !s.ClosingGate && !s.Track && !s.Task && (s.Lane == "" || s.Lane == "agents" || s.Lane == "commander")
}

// stepDone: closed, dropped, or holding a finished result being closed.
func stepDone(s livedata.SnapStep) bool {
	return (s.Status != "open" && s.Status != "in_progress") || s.Superseded || s.Withdrawn ||
		(s.ResultReadyAt > 0 && !agentDied(s))
}

// stepBlocked: something else must happen first: a blocker or child step not
// done, a wait (an agent asleep on its event counts as alive until it fires),
// the user's card or answer, a question, a hold, or execution switched off.
// A paused or held parent blocks too (autoDispatchJobs skips its subtree).
func stepBlocked(s livedata.SnapStep, byID map[string]livedata.SnapStep) bool {
	if s.Hold || s.State == "needs-user" || s.QuestionWaiting || s.HumanAnswerPending || (s.ExecutionMode != "" && s.ExecutionMode != "active") {
		return true
	}
	for _, w := range s.TypedWaits {
		if !beads.IsManagerHandoff(w) {
			return true
		}
	}
	for _, c := range s.Children {
		if c.Status != "closed" && c.State != "done" && !c.Superseded {
			return true
		}
	}
	for _, id := range s.BlockedBy {
		dep, ok := byID[id]
		if !ok {
			// A blocker in another workstream: the snapshot lists it as done or not.
			if !slices.Contains(s.DoneBlockers, id) {
				return true
			}
			continue
		}
		if dep.Status != "closed" && dep.State != "done" && !dep.Superseded {
			return true
		}
	}
	return false
}

// liveAgent: an agent is running on the step, starting, or queued for it.
func liveAgent(s livedata.SnapStep) bool {
	return s.ExecutionReady || s.OwnerState == livedata.OwnerWorking || s.LiveRuntime != ""
}

// agentDied: the step's last agent ended without a report (its result is
// cmdrd's note of that, or the run stopped), so the next check starts another.
func agentDied(s livedata.SnapStep) bool {
	for _, p := range []string{"Execution exited without", "Execution ended with an error", "Mac execution ended"} {
		if strings.HasPrefix(s.ResultLine, p) {
			return true
		}
	}
	return s.ResultReadyAt == 0 && (s.OwnerState == livedata.OwnerDied || strings.HasPrefix(s.ExecutionError, store.AgentStopped))
}

// managerRunning: role names a Manager whose chat is up. With none recorded
// (the Commander plans directly) there is no Manager to wait for.
func managerRunning(snap livedata.Snapshot, role string) bool {
	if role == "" {
		return false
	}
	a, ok := snap.Agents[role]
	return ok && a.Detail != "Process is not running" && (a.State == store.AgentWorking || a.State == store.AgentIdle)
}

func dispatchReceipt(label string) bool {
	w, ok := beads.ParseWaitLabel(label)
	return ok && beads.IsManagerDispatch(w)
}

func (d *Daemon) runAutoDispatch(ctx context.Context) {
	t := time.NewTicker(autoDispatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		d.autoDispatchOnce(ctx)
	}
}

// autoDispatchOnce starts an agent on every unblocked agent step.
func (d *Daemon) autoDispatchOnce(ctx context.Context) {
	snap := d.live.Snapshot(ctx)
	if snap.Degraded != nil || snap.OrchestrationMode != "active" {
		return
	}
	for _, j := range autoDispatchJobs(snap) {
		if j.died {
			n, err := d.store.DeathsInARow(ctx, j.projectID, j.bead)
			if err == nil && n >= maxDeaths {
				deathCard(ctx, j, n, cmdrCommand)
				continue
			}
		}
		ok, err := d.store.DispatchReady(ctx, j.projectID, j.bead, dispatchHandoff)
		switch {
		case err != nil:
			log.Printf("auto-dispatch: %s %s: %v", j.project, j.bead, err)
		case ok:
			log.Printf("auto-dispatch: %s %s %q is ready for an agent", j.project, j.bead, j.title)
		}
	}
}

// dispatchHandoff: a Manager custody receipt, cleared when cmdrd starts the step.
func dispatchHandoff(label string) bool {
	w, ok := beads.ParseWaitLabel(label)
	return ok && beads.IsManagerHandoff(w)
}
