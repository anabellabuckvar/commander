package daemon

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/store"
)

const autoAcceptEvery = 15 * time.Second

// acceptJob closes one finished agent step.
type acceptJob = store.AcceptCandidate

// acceptReason is the close reason of an accepted agent step: its own report.
func acceptReason(c acceptJob) string {
	line := c.Line
	if len(line) > 300 {
		line = line[:300]
	}
	return "Accepted automatically: " + line
}

// acceptResults closes every agent step whose run ended with a clean final
// report, through the validated write path, and reports how many it closed.
// The report stays on the step as its result.
func acceptResults(ctx context.Context, list func(context.Context) ([]acceptJob, error), bc beads.Client) int {
	jobs, err := list(ctx)
	if err != nil {
		log.Printf("auto-accept: list: %v", err)
		return 0
	}
	n := 0
	for _, j := range jobs {
		if err := bc.CloseWithReason(ctx, j.Path, j.Bead, acceptReason(j)); err != nil {
			log.Printf("auto-accept: %s: %v", j.Bead, err)
			continue
		}
		log.Printf("auto-accept: %s %q closed on its agent's final report", j.Bead, j.Title)
		n++
	}
	return n
}

// runAutoAccept accepts finished agent steps every autoAcceptEvery, and starts
// the steps they unblock at once.
func (d *Daemon) runAutoAccept(ctx context.Context) {
	t := time.NewTicker(autoAcceptEvery)
	defer t.Stop()
	for {
		if d.attempted == nil {
			d.attempted = &answerTries{}
		}
		applyAnswers(ctx, d.store.PendingAnswers, d.attempted)
		if acceptResults(ctx, d.store.AgentResultsToAccept, d.beads) > 0 {
			d.store.NotifyStateChanged(ctx)
			d.autoDispatchOnce(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// answerTries remembers answers cmdrd already tried to apply, so a failing
// one is retried at most every ten minutes.
type answerTries struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// applyAnswers finishes every recorded answer of the user's that its step has
// not taken yet, through the same command the Commander used to run:
// applying the user's own answer needs no one's judgment.
func applyAnswers(ctx context.Context, list func(context.Context) ([]store.PendingAnswer, error), tries *answerTries) {
	answers, err := list(ctx)
	if err != nil || len(answers) == 0 {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmdr := filepath.Join(filepath.Dir(self), "cmdr")
	for _, a := range answers {
		key := a.Project + ":" + a.Bead
		tries.mu.Lock()
		if tries.last == nil {
			tries.last = map[string]time.Time{}
		}
		recent := time.Since(tries.last[key]) < 10*time.Minute
		if !recent {
			tries.last[key] = time.Now()
		}
		tries.mu.Unlock()
		if recent {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(c, cmdr, "internal-resolve-needs", "--project", a.Project, "--bead", a.Bead, "--action", a.Action, "--resume-only")
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			log.Printf("apply-answer: %s %s: %v: %s", a.Project, a.Bead, err, firstLine(string(out), 300))
			continue
		}
		log.Printf("apply-answer: %s %s applied the user's recorded answer", a.Project, a.Bead)
	}
}
