package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/example-user/commander/internal/store"
)

// CutoverGrace is how long an agent asked to save its work and stop gets
// before cmdrd stops its process.
const CutoverGrace = 10 * time.Minute

// executionCutover carries out the cutovers Commander records (a runtime
// switch or a stop): it tells the running agent once to save its work and end
// its turn, and stops the process after CutoverGrace. The run's exit then
// resumes the step on the new runtime, or leaves it stopped (store).
type executionCutover struct {
	store interface {
		ExecutionCutovers(context.Context) ([]store.ExecutionCutover, error)
		RecordCutoverStep(context.Context, store.ExecutionCutover, string, string) error
	}
	send func(ctx context.Context, role, text string) error
	stop func(ctx context.Context, role string) error
	now  func() time.Time
}

func (c executionCutover) run(ctx context.Context) {
	t := time.NewTicker(redirectInterval)
	defer t.Stop()
	for {
		c.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c executionCutover) pass(ctx context.Context) {
	all, err := c.store.ExecutionCutovers(ctx)
	if err != nil {
		log.Printf("execution cutover: %v", err)
		return
	}
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	for _, x := range all {
		if !x.Running {
			continue
		}
		switch {
		case x.ToldAt == 0:
			text := cutoverMessage(x)
			if err := c.send(ctx, x.Agent, text); err != nil {
				log.Printf("execution cutover: tell %s about %s: %v", x.Agent, x.BeadID, err)
				continue
			}
			if err := c.store.RecordCutoverStep(ctx, x, store.CutoverToldKind, "cmdrd told the agent: "+text); err != nil {
				log.Printf("execution cutover: record told %s: %v", x.BeadID, err)
			}
		case x.ForcedAt == 0 && now.Unix()-x.ToldAt >= int64(CutoverGrace/time.Second):
			// The engine stops this one role's process (explicit pid), never a group.
			stopCtx, cancel := context.WithTimeout(ctx, time.Minute)
			err := c.stop(stopCtx, x.Agent)
			cancel()
			if err != nil {
				log.Printf("execution cutover: stop %s on %s: %v", x.Agent, x.BeadID, err)
			}
			if err := c.store.RecordCutoverStep(ctx, x, store.CutoverForcedKind, fmt.Sprintf("cmdrd stopped %s: it had not stopped %s after being asked", x.Agent, CutoverGrace)); err != nil {
				log.Printf("execution cutover: record stop %s: %v", x.BeadID, err)
			}
		}
	}
}

func cutoverMessage(x store.ExecutionCutover) string {
	why := ""
	if x.Reason != "" {
		why = " (" + x.Reason + ")"
	}
	save := "Save your work now: commit your work in progress on your branch and push it, then write a one-line bead comment starting \"Final report:\" saying where you stopped and what is next, then end your turn. Start nothing new."
	if x.To == "" {
		return fmt.Sprintf("[Commander] Stop work on %s%s. %s Nobody continues this step until Commander re-plans it. cmdrd stops this process in %s.", x.BeadID, why, save, CutoverGrace)
	}
	return fmt.Sprintf("[Commander] This step (%s) is moving from %s to %s%s. %s A %s agent continues from your branch and your report. cmdrd stops this process in %s.", x.BeadID, x.From, x.To, why, save, x.To, CutoverGrace)
}
