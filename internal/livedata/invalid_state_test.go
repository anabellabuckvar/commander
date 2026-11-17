package livedata

import "testing"

// The audit's violations show on the workstream and its track as red findings.
func TestInvalidStateIsARedFinding(t *testing.T) {
	ws := SnapWorkstream{BeadID: "w", Project: "p", Steps: []SnapStep{{BeadID: "w.1", Title: "Track", Status: "open", Children: []SnapStep{
		{BeadID: "w.1.2", Title: "Propose the test cases", Status: "open"},
		{BeadID: "w.1.3", Title: "Approve the test cases", Status: "open", Gate: true, Approvers: []string{"user"}, BlockedBy: []string{"w.1.2"}},
	}}}}
	s := snapOf(ws, idle)
	s.Workstreams[0].Project = "p"
	s.InvalidState = []InvalidState{{Project: "p", Rule: "hidden-review", Beads: []string{"w.1.2", "w.1.3"}, Why: "hidden", Fix: "add a check"}}
	d := Board(s, nowSec)
	found := false
	for _, f := range d.Workstreams[0].Diagram.Findings {
		if f.Check == CheckInvalidState+"hidden-review" && f.Bead == "w.1.2" && FindingKind(f) == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v", d.Workstreams[0].Diagram.Findings)
	}
	s.InvalidState[0].Project = "other"
	for _, f := range Board(s, nowSec).Workstreams[0].Diagram.Findings {
		if f.Check == CheckInvalidState+"hidden-review" {
			t.Fatal("another project's violation drawn here")
		}
	}
}
