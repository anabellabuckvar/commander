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
	for _, p := range projects {
		vs, err := w.shapeReport(ctx, p.Path)
		if err != nil {
			log.Printf("daemon: state audit %s: %v", p.Name, err)
			continue
		}
		for _, v := range vs {
			if v.Rule == "parked-without-reason" {
				continue // advisory: never a red row
			}
			all = append(all, livedata.InvalidState{Project: p.Name, Rule: v.Rule, Beads: v.Beads, Why: v.Why, Fix: v.Fix})
			key := "invalid|" + p.Name + "|" + v.Key()
			if w.woken[key] {
				continue
			}
			subject := fmt.Sprintf("[invalid state] %s: %s", v.Rule, strings.Join(v.Beads, ", "))
			body := fmt.Sprintf("Project %s: %s\nFix: %s\nThe board shows it as a red \"invalid state: %s\" finding until it is fixed.", p.Name, v.Why, v.Fix, v.Rule)
			if err := w.mail(ctx, "commander", subject, body); err != nil {
				continue
			}
			w.woken[key] = true
		}
	}
	w.invalid.Store(&all)
}

// InvalidState is the last audit's violations, for the snapshot.
func (w *waitWatcher) InvalidState() []livedata.InvalidState {
	if p := w.invalid.Load(); p != nil {
		return *p
	}
	return nil
}
