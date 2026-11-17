package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
)

// shapeReportFunc runs the write path's validator over a project's open trees.
type shapeReportFunc func(ctx context.Context, dir string) ([]beads.ShapeViolation, error)

func defaultShapeReport(ctx context.Context, dir string) ([]beads.ShapeViolation, error) {
	if !beads.PostgresConfigured() {
		return nil, nil
	}
	return beads.PlanShapeReport(ctx, dir, nil)
}

// auditState runs the same validator every write runs, over every open tree,
// each poll. The board shows each violation as an "invalid state" finding; a
// violation not seen before goes to the Commander's queue once.
func (w *waitWatcher) auditState(ctx context.Context, projects []store.Project) {
	if w.shapeReport == nil {
		return
	}
	var all []livedata.InvalidState
	w.qmu.Lock()
	w.projectName = map[string]string{}
	for _, p := range projects {
		w.projectName[p.Path] = p.Name
	}
	w.qmu.Unlock()
	for _, p := range projects {
		vs, err := w.shapeReport(ctx, p.Path)
		if err != nil {
			log.Printf("daemon: state audit %s: %v", p.Name, err)
			continue
		}
		for _, v := range vs {
			if v.Rule == "parked-without-reason" || v.Rule == "no-owner-tag" {
				// advisory on old steps: a new step without an owner tag is refused at
				// write time; old ones show their owner from the run they had.
				continue
			}
			all = append(all, livedata.InvalidState{Project: p.Name, Rule: v.Rule, Beads: v.Beads, Why: v.Why, Fix: v.Fix})
			w.queueInvalid(ctx, p.Name, v.Rule, v.Beads, v.Why, v.Fix)
		}
	}
	w.invalid.Store(&all)
}

// InvalidState is the last audit's violations, for the snapshot.
func (w *waitWatcher) InvalidState() []livedata.InvalidState {
	var out []livedata.InvalidState
	if p := w.invalid.Load(); p != nil {
		out = append(out, *p...)
	}
	w.qmu.Lock()
	for _, v := range w.overdue {
		out = append(out, v)
	}
	w.qmu.Unlock()
	return out
}

// projectOf is the name of the project a bead belongs to, from its id prefix
// matched against the projects the last audit listed ("" when unknown).
func (w *waitWatcher) projectOf(b beads.Bead) string {
	w.qmu.Lock()
	defer w.qmu.Unlock()
	best := ""
	for _, name := range w.projectName {
		if strings.HasPrefix(b.ID, name+"-") && len(name) > len(best) {
			best = name
		}
	}
	return best
}

// queueInvalid puts one invalid-structure item in the Commander's queue, once
// per project, rule and bead: fixing bead structure is the Commander's job.
func (w *waitWatcher) queueInvalid(ctx context.Context, project, rule string, ids []string, why, fix string) {
	if len(ids) == 0 {
		return
	}
	key := "invalid|" + project + "|" + rule + "|" + ids[0]
	w.qmu.Lock()
	if w.queued == nil {
		w.queued = map[string]bool{}
	}
	done := w.queued[key]
	w.qmu.Unlock()
	if done {
		return
	}
	subject := fmt.Sprintf("[invalid state] %s: %s", rule, strings.Join(ids, ", "))
	body := fmt.Sprintf("Project %s: %s\nFix: %s\nThe board shows it as a red \"invalid-state: %s\" finding until it is fixed.", project, why, fix, rule)
	if err := w.mail(ctx, "commander", subject, body); err != nil {
		return
	}
	w.qmu.Lock()
	w.queued[key] = true
	w.qmu.Unlock()
}

// queueBoardInvalid queues the board's invalid-state findings (a step with no
// valid owner, a line naming the wrong actor) the same way.
func (w *waitWatcher) queueBoardInvalid(ctx context.Context, snap *livedata.Snapshot) {
	if w == nil || snap == nil || snap.Display == nil || w.mail == nil {
		return
	}
	for _, r := range snap.Display.Workstreams {
		if r.Diagram == nil {
			continue
		}
		for _, f := range r.Diagram.Findings {
			if rule, ok := strings.CutPrefix(f.Check, livedata.CheckInvalidState); ok {
				w.queueInvalid(ctx, r.Project, rule, []string{f.Bead}, f.Why, f.Fix)
			}
		}
	}
}
