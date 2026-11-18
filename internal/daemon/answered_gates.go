package daemon

import (
	"context"
	"log"
	"regexp"

	"github.com/example-user/commander/internal/store"
)

// mergeGate: a gate that merges a PR.
var mergeGate = regexp.MustCompile(`(?i)\bmerg(e|ing)\b`)

var notYet = regexp.MustCompile(`(?i)\b(not now|not yet|later|wait|hold|no)\b`)

var dropChoice = regexp.MustCompile(`(?i)^\s*(drop|cancel|skip|withdraw|remove|not needed|no longer)`)

// rejects: an answer that turns the gate down for good ("No", "Close it",
// "Don't merge", "Reject", "Abandon it"), as opposed to holding it ("Not now").
var rejects = regexp.MustCompile(`(?i)^\s*(no\b|nope\b|don'?t\b|do not\b|close\b|reject|decline|abandon|kill\b|scrap\b|drop\b|cancel\b|not needed|no longer)`)

// holds: an answer that only puts the gate off.
var holds = regexp.MustCompile(`(?i)\b(not now|not yet|later|wait|hold|pause)\b`)

// gateCloseReason is how the user's answer closes their gate, or "" when the
// answer neither approves, drops nor answers it (it stays for the Commander
// to turn into steps).
func gateCloseReason(g store.AnsweredGate) string {
	reason, _ := gateSettlement(g)
	return reason
}

// gateSettlement is gateCloseReason, and whether the answer rejected the gate
// (its PR is closed and what depended only on it is superseded too).
func gateSettlement(g store.AnsweredGate) (reason string, rejected bool) {
	answer := answerText(g.Choice, g.Text)
	said := g.Choice
	if said == "" {
		said = g.Text
	}
	if !g.Approved && !holds.MatchString(said) && (rejects.MatchString(said) || dropChoice.MatchString(said)) {
		verb := "answered"
		if g.Choice != "" {
			verb = "chose"
		}
		return "Superseded: the user " + verb + " “" + answer + "”", true
	}
	return gateCloseReasonOld(g, answer), false
}

func gateCloseReasonOld(g store.AnsweredGate, answer string) string {
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
// rejected, when not nil, finishes a gate the user turned down: its PR and what
// depended only on it.
func closeAnsweredGates(ctx context.Context, list func(context.Context) ([]store.AnsweredGate, error), closeStep func(ctx context.Context, project, bead, reason string) error, rejected ...func(ctx context.Context, g store.AnsweredGate, reason string)) int {
	gates, err := list(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, g := range gates {
		reason, no := gateSettlement(g)
		if reason == "" {
			continue
		}
		if err := closeStep(ctx, g.Project, g.Bead, reason); err != nil {
			log.Printf("answered-gate: %s: %v", g.Bead, err)
			continue
		}
		log.Printf("answered-gate: %s %q closed: %s", g.Bead, g.Title, reason)
		if no {
			for _, f := range rejected {
				f(ctx, g, reason)
			}
		}
		n++
	}
	return n
}
