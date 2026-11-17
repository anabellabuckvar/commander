package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/example-user/commander/internal/chat"
	"github.com/example-user/commander/internal/store"
)

// redirectInterval is how often running executions are checked for a changed bead.
const redirectInterval = 10 * time.Second

// executionRedirector tells a running agent when Commander changes its bead,
// in the agent's own session, once per plan revision.
type executionRedirector struct {
	store interface {
		ChangedExecutionPlans(context.Context) ([]store.PlanChange, error)
		RecordExecutionRedirect(context.Context, store.PlanChange, string) error
	}
	send func(ctx context.Context, role, text string) error
	// dead backs off, then gives up, telling a role with no running process
	// (nil: retry every pass).
	dead *unreachable
	now  func() time.Time
}

func (r executionRedirector) run(ctx context.Context) {
	t := time.NewTicker(redirectInterval)
	defer t.Stop()
	for {
		r.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r executionRedirector) pass(ctx context.Context) {
	changes, err := r.store.ChangedExecutionPlans(ctx)
	if err != nil {
		log.Printf("execution redirect: %v", err)
		return
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	for _, c := range changes {
		key := fmt.Sprintf("%s|%s|%d", c.Agent, c.BeadID, c.CurrentRevision)
		if !r.dead.due(key, now) {
			continue
		}
		text := redirectMessage(c)
		if err := r.send(ctx, c.Agent, text); err != nil {
			if r.dead != nil && errors.Is(err, chat.ErrNotRunning) {
				first, giveUp := r.dead.failed(key, now)
				switch {
				case giveUp:
					// Handled: the run's process is gone, so nobody can be told;
					// its exit and liveness settle the step.
					if err := r.store.RecordExecutionRedirect(ctx, c, "Not delivered: "+c.Agent+" has no running process, so it was not told: "+text); err != nil {
						log.Printf("execution redirect: record %s: %v", c.BeadID, err)
					}
					log.Printf("execution redirect: gave up telling %s about %s: no running process after %d tries", c.Agent, c.BeadID, len(unreachableDelays)+1)
				case first:
					log.Printf("execution redirect: tell %s about %s: %v (retrying with backoff)", c.Agent, c.BeadID, err)
				}
				continue
			}
			log.Printf("execution redirect: tell %s about %s: %v", c.Agent, c.BeadID, err)
			continue
		}
		r.dead.clear(key)
		if err := r.store.RecordExecutionRedirect(ctx, c, text); err != nil {
			log.Printf("execution redirect: record %s: %v", c.BeadID, err)
		}
	}
}

func redirectMessage(c store.PlanChange) string {
	read := fmt.Sprintf("cmdr bead --project-dir '%s' show %s", strings.ReplaceAll(c.ProjectPath, "'", `'"'"'`), c.BeadID)
	if c.Status == "closed" || c.Status == "deferred" {
		return fmt.Sprintf("[Commander] Your assigned bead %s is now %s. Stop this work: record a Final report with what you finished, what is incomplete and where you stopped, then stop. Read it with: %s", c.BeadID, c.Status, read)
	}
	return fmt.Sprintf("[Commander] Your assigned bead %s changed after you started (plan revision %d -> %d). Re-read it now: %s. If your current work still fits its brief and acceptance, continue. If it no longer fits, change course to the new brief. If the change is unclear, ask with cmdr question ask using plan revision %d.", c.BeadID, c.ClaimedRevision, c.CurrentRevision, read, c.CurrentRevision)
}
