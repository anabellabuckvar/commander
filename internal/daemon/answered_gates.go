package daemon

import (
	"context"
	"log"
	"regexp"

	"github.com/example-user/commander/internal/store"
)

var notYet = regexp.MustCompile(`(?i)\b(not now|not yet|later|wait|hold|no)\b`)

var dropChoice = regexp.MustCompile(`(?i)^\s*(drop|cancel|skip|withdraw|remove|not needed|no longer)`)

// gateCloseReason is how the user's answer closes their gate, or "" when the
// answer neither approves, drops nor answers it (it stays for the Commander
// to turn into steps).
func gateCloseReason(g store.AnsweredGate) string {
	answer := answerText(g.Choice, g.Text)
	// Only an answer that approves or drops the gate settles it. Anything else
	// ("Not now", "Not yet", a note) leaves it for the Commander to restructure:
	// closing it would start the work the user held back.
	switch {
	case dropChoice.MatchString(g.Choice):
		return "Superseded: the user chose “" + answer + "”"
	case !g.Gate:
		// A step that is not a gate (a dying agent's card) only closes on a drop.
		return ""
	case g.Approved && !notYet.MatchString(g.Choice):
		return "Approved by the user: " + answer
	}
	return ""
}

// closeAnsweredGates closes the user's answered gates the answer settles, so
// the work after them starts at once instead of waiting on the Commander.
func closeAnsweredGates(ctx context.Context, list func(context.Context) ([]store.AnsweredGate, error), closeStep func(ctx context.Context, project, bead, reason string) error) int {
	gates, err := list(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, g := range gates {
		reason := gateCloseReason(g)
		if reason == "" {
			continue
		}
		if err := closeStep(ctx, g.Project, g.Bead, reason); err != nil {
			log.Printf("answered-gate: %s: %v", g.Bead, err)
			continue
		}
		log.Printf("answered-gate: %s %q closed: %s", g.Bead, g.Title, reason)
		n++
	}
	return n
}
