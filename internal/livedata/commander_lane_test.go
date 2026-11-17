package livedata

import (
	"strings"
	"testing"

	"github.com/example-user/commander/internal/nextactor"
)

// Track 3.4 on Nov 17: the Commander's own check before Eric's merge card was
// created as a gate approved by commander (vibe-apps-en1h.12.25), which drew in
// the Reviewers column (people), and then as lane:you (12.26), which drew in
// Eric's own column. The Commander's step belongs in the Commander column.
func TestCommanderChecksDrawInTheAgentsColumn(t *testing.T) {
	gate := SnapStep{BeadID: "vibe-apps-en1h.12.25", Title: "Commander review: redesign PR head, screenshots and Devin threads before Eric's merge card",
		Status: "open", Gate: true, Approvers: []string{"commander"}}
	if h := restingHand(gate); h.actor != ActorOperators {
		t.Fatalf("a Commander-approved check draws in %q, want the agents (a review agent runs it)", h.actor)
	}
	lane := SnapStep{BeadID: "x", Title: "Commander checks the green redesign PR", Status: "open", Lane: "commander"}
	if h := restingHand(lane); h.actor != ActorOperators {
		t.Fatalf("lane:commander draws in %q, want the agents (a review agent runs it)", h.actor)
	}
	owned := SnapStep{BeadID: "y", Title: "Commander checks", Status: "open", Owner: "commander"}
	if h := restingHand(owned); h.actor != ActorOperators {
		t.Fatalf("a Commander-owned step draws in %q, want the agents", h.actor)
	}
	people := SnapStep{BeadID: "z", Title: "Matt reviews", Status: "open", Gate: true, Approvers: []string{"mfournierfira"}}
	if h := restingHand(people); h.actor != ActorReviewers {
		t.Fatalf("a person's review draws in %q, want Reviewers", h.actor)
	}

	wrong := SnapStep{BeadID: "vibe-apps-en1h.12.26", Title: "Commander checks the green redesign PR (head, screenshots, Devin threads) before Eric's merge card",
		Status: "open", Lane: "you", Waiting: "operator-x"}
	ws := SnapWorkstream{Project: "vibe-apps", BeadID: "ws", Steps: []SnapStep{gate, wrong}}
	found := map[string]bool{}
	for _, f := range CheckDiagram(ws, ws.BeadID) {
		if f.Check == "commander-lane" {
			found[f.Bead] = true
		}
	}
	if !found["vibe-apps-en1h.12.26"] {
		t.Fatalf("the Commander's step in the user's column was not flagged: %v", found)
	}
	if found["vibe-apps-en1h.12.25"] {
		t.Fatalf("the Commander's own gate, now in its column, was flagged")
	}
}

// Track 3.2 on Nov 17 read "Commander to review the finished result" on
// vibe-apps-596t.12.6, but nothing on the graph said a review was pending. The
// row must point at something drawn: the step reads the review on its node, and
// a review whose step is not drawn at all is a finding.
func TestPendingReviewIsDrawnOnItsStep(t *testing.T) {
	step := SnapStep{BeadID: "vibe-apps-596t.12.6", Title: "Grafana alerting: live check after the morning prod release", Status: "open", ResultReadyAt: 1794927332}
	ws := SnapWorkstream{Project: "vibe-apps", BeadID: "w", Steps: []SnapStep{step}}
	lead := &nextactor.Row{Lead: nextactor.Result{Actor: nextactor.Commander, Reason: "review_result", Bead: step.BeadID, Text: "Commander to review the finished result"}}
	r := BoardRow{Actor: lead, Diagram: &Diagram{ID: "w", Steps: []DiagramStep{{ID: step.BeadID, Kind: "waiting", Text: "Waiting for prod tag"}}}}
	showPendingReviews(ws, &r)
	if got := r.Diagram.Steps[0].Text; !strings.HasPrefix(got, "Commander to review its result (since ") {
		t.Fatalf("node reads %q, want the pending review with its time", got)
	}
	if len(r.Diagram.Findings) != 0 {
		t.Fatalf("a drawn review was flagged: %+v", r.Diagram.Findings)
	}

	hidden := BoardRow{Actor: lead, Diagram: &Diagram{ID: "w"}}
	showPendingReviews(ws, &hidden)
	if len(hidden.Diagram.Findings) != 1 || hidden.Diagram.Findings[0].Check != CheckHiddenReview {
		t.Fatalf("a review on an undrawn step was not flagged: %+v", hidden.Diagram.Findings)
	}
}
