package daemon

import (
	"context"
	"log"
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
				if dispatchable(s, byID) && !running[key{ws.ProjectID, s.BeadID}] && !asked[ws.Project+"\x00"+s.BeadID] {
					out = append(out, dispatchJob{projectID: ws.ProjectID, project: ws.Project, bead: s.BeadID, title: s.Title})
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

func dispatchable(s livedata.SnapStep, byID map[string]livedata.SnapStep) bool {
	if s.Status != "open" || s.Superseded || s.Withdrawn || s.Hold || s.Gate || s.ClosingGate || s.Track || s.Task ||
		s.ExecutionReady || s.ExecutionMode != "active" || s.ResultReadyAt > 0 || s.QuestionWaiting ||
		s.HumanAnswerPending || s.State == "needs-user" || s.OwnerState == "working" ||
		s.NoBrief || (s.Lane != "" && s.Lane != "agents" && s.Lane != "commander") { // a person's task (lane you or elsewhere) is never an agent's
		return false
	}
	for _, w := range s.TypedWaits {
		if !beads.IsManagerHandoff(w) {
			return false // another wait holds it
		}
	}
	for _, c := range s.Children {
		if c.Status != "closed" && c.State != "done" && !c.Superseded {
			return false
		}
	}
	for _, id := range s.BlockedBy {
		dep, ok := byID[id]
		if !ok || (dep.Status != "closed" && dep.State != "done" && !dep.Superseded) {
			return false
		}
	}
	return true
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
