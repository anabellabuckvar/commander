package livedata

import (
	"strings"

	"github.com/example-user/commander/internal/nextactor"
)

// ActorMismatch is a board line that names an actor its step does not have:
// the word Commander (or Manager) on a step not tagged for the Commander.
// Only an invalid-structure item may name the Commander, since fixing beads
// is the Commander's one job on the board.
const ActorMismatch = "actor-mismatch"

// lineMismatch: line names the Commander but lead is not a structure fix.
func lineMismatch(line string, lead *nextactor.Row) bool {
	if !namesCommander(line) {
		return false
	}
	if lead != nil && (lead.Lead.Actor == nextactor.Commander || strings.HasPrefix(lead.Lead.Reason, nextactor.InvalidPrefix) || strings.HasPrefix(lead.Lead.Reason, nextactor.StructurePrefix)) {
		return false
	}
	return true
}

// addActorChecks puts a red finding on r for every row or track line that
// names an actor its step is not tagged for, and for every lead that is an
// invalid state (a step with no valid owner).
func (b board) addActorChecks(ws SnapWorkstream, r *BoardRow) {
	if r.Diagram == nil {
		return
	}
	type line struct {
		id, title, text string
		actor           *nextactor.Row
	}
	lines := []line{{r.BeadID, r.Title, r.Text, r.Actor}}
	for _, t := range r.AllTracks() {
		lines = append(lines, line{t.BeadID, t.Title, t.Text, t.Actor})
	}
	seen := map[string]bool{}
	for _, l := range lines {
		bead := l.id
		if l.actor != nil && l.actor.Lead.Bead != "" {
			bead = l.actor.Lead.Bead
		}
		if lineMismatch(l.text, l.actor) && !seen["m"+bead] {
			seen["m"+bead] = true
			r.Diagram.Findings = append(r.Diagram.Findings, DiagramFinding{Check: CheckInvalidState + ActorMismatch, Bead: bead, Title: plainTitle(l.title),
				Why: "the line reads \"" + l.text + "\", naming the Commander, but its step is not a Commander step",
				Fix: "cmdrd derives the line from the step's owner tag; mail Commander this finding if the step's tag is wrong"})
		}
		if l.actor != nil && strings.HasPrefix(l.actor.Lead.Reason, nextactor.InvalidPrefix) && !seen["i"+bead] {
			seen["i"+bead] = true
			rule := strings.TrimPrefix(l.actor.Lead.Reason, nextactor.InvalidPrefix)
			r.Diagram.Findings = append(r.Diagram.Findings, DiagramFinding{Check: CheckInvalidState + rule, Bead: bead, Title: plainTitle(l.title),
				Why: l.actor.Lead.Text, Fix: invalidFix(rule, bead)})
		}
	}
}

func invalidFix(rule, bead string) string {
	switch rule {
	case "no-card":
		return "put the step's question on Eric's list (cmdr internal-set-objective --bead " + bead + " with the ask), or retag it lane:agents if an agent does it"
	case "no-open-step":
		return "plan the track's next step with cmdr plan apply, or close the track if it is finished"
	case "no-brief":
		return "give " + bead + " a description (what to do) and acceptance (what counts as done) so an agent can start"
	}
	return "fix the step's tags with cmdr plan apply"
}
