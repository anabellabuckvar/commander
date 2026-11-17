package livedata

import (
	"testing"
)

// Track 3.4 on Nov 17: the Commander's own check before Eric's merge card was
// created as a gate approved by commander (vibe-apps-en1h.12.25), which drew in
// the Reviewers column (people), and then as lane:you (12.26), which drew in
// Eric's own column. The Commander's step belongs in the Commander column.
func TestCommanderOwnStepsDrawInTheCommanderColumn(t *testing.T) {
	gate := SnapStep{BeadID: "vibe-apps-en1h.12.25", Title: "Commander review: redesign PR head, screenshots and Devin threads before Eric's merge card",
		Status: "open", Gate: true, Approvers: []string{"commander"}}
	if h := restingHand(gate); h.actor != ActorManager {
		t.Fatalf("a gate the Commander approves draws in %q, want the Commander column (%q)", h.actor, ActorManager)
	}
	lane := SnapStep{BeadID: "x", Title: "Commander checks the green redesign PR", Status: "open", Lane: "commander"}
	if h := restingHand(lane); h.actor != ActorManager {
		t.Fatalf("lane:commander draws in %q, want the Commander column", h.actor)
	}
	owned := SnapStep{BeadID: "y", Title: "Commander checks", Status: "open", Owner: "commander"}
	if h := restingHand(owned); h.actor != ActorManager {
		t.Fatalf("a Commander-owned step draws in %q, want the Commander column", h.actor)
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
