package livedata

import (
	"strings"
	"testing"

	"github.com/example-user/commander/internal/nextactor"
)

// Every row and track line names only its step's owner: no line says
// Commander (or Manager) unless the step is an invalid-structure item.
func assertActorsMatch(t *testing.T, d Display) {
	t.Helper()
	for _, r := range d.Workstreams {
		lines := []struct {
			text string
			a    *nextactor.Row
		}{{r.Text, r.Actor}}
		for _, tr := range r.AllTracks() {
			lines = append(lines, struct {
				text string
				a    *nextactor.Row
			}{tr.Text, tr.Actor})
		}
		for _, l := range lines {
			if lineMismatch(l.text, l.a) {
				t.Errorf("row %d line %q names the Commander for a step that is not its", r.N, l.text)
			}
			for _, banned := range []string{"plan the next step", "review its result", "review the finished result"} {
				if strings.Contains(l.text, banned) {
					t.Errorf("row %d line %q is a fallback line", r.N, l.text)
				}
			}
		}
	}
}

// OM Hub on Nov 17: the inventory's agent finished with a clean final report;
// the next agent step is not started. Nothing on the board is the Commander's.
func TestOMHubBoardNamesNoCommander(t *testing.T) {
	ws := SnapWorkstream{BeadID: "om", Project: "p", Title: "Orders Management Hub on Temporal", Steps: []SnapStep{
		{BeadID: "om.1", Title: "Inventory every OM Hub background job", Status: "open", ResultRunID: 7, ResultReadyAt: nowSec - 60, ResultLine: "Final report: 14 jobs inventoried"},
		{BeadID: "om.2", Title: "Write the OM Hub cutover plan", Status: "open", BlockedBy: []string{"om.1"}},
		{BeadID: "om.3", Title: "Commander checks the OM Hub inventory and plan", Status: "open", Lane: "commander", BlockedBy: []string{"om.2"}},
	}}
	s := snapOf(ws, idle)
	s.OrchestrationMode = "active"
	d := Board(s, nowSec)
	assertActorsMatch(t, d)
	if a := d.Workstreams[0].Actor; a == nil || a.Lead.Actor == nextactor.Commander || a.Lead.Bead != "om.1" || a.Lead.Reason != "result_accepting" {
		t.Fatalf("lead = %+v", a)
	}
	// Once the inventory closes, the plan step is the next agent's.
	s.Workstreams[0].Steps[0].Status, s.Workstreams[0].Steps[0].State = "closed", "done"
	d = Board(s, nowSec)
	assertActorsMatch(t, d)
	if a := d.Workstreams[0].Actor; a == nil || a.Lead.Bead != "om.2" || a.Lead.Actor != nextactor.Agent {
		t.Fatalf("after accept, lead = %+v", a)
	}
}

func TestActorMismatchIsARedFinding(t *testing.T) {
	agent := &nextactor.Row{Lead: nextactor.Result{Actor: nextactor.Agent, Reason: "executing", Bead: "w.1"}}
	if !lineMismatch("Commander to review redesign result", agent) || lineMismatch("Agent working on the redesign", agent) {
		t.Fatal("mismatch check wrong")
	}
	invalid := &nextactor.Row{Lead: nextactor.Result{Actor: nextactor.Stuck, Reason: nextactor.InvalidPrefix + "no-card"}}
	if lineMismatch("Commander: put the card up", invalid) {
		t.Fatal("a structure item may name the Commander")
	}
}
