package livedata

import (
	"testing"

	"github.com/example-user/commander/internal/beads"
)

// Track 4.8 on Nov 16: its "Confirm done" read red "Needs you" while the
// track's speedups agent was still working. Gates wait only for siblings listed
// before them, and a Confirm done made first precedes them all.
func TestNeedsYouList_ConfirmDoneWaitsForEverySibling(t *testing.T) {
	d := fleet()
	d.Objectives = append(d.Objectives,
		ObjectiveRow{BeadID: "a.0", ParentBeadID: "a", Title: "Confirm done", ProjectID: 1, State: beads.StateNotStarted,
			ClosingGate: true, Gate: true, Approvers: []string{"user"}, GateNote: "Everything shipped."},
	)
	listed := func() bool {
		for _, n := range NeedsYouList(d) {
			if n.BeadID == "a.0" {
				return true
			}
		}
		return false
	}
	if listed() {
		t.Fatal("Confirm done asked while its sibling steps are open")
	}
	for i := range d.Objectives {
		if d.Objectives[i].ParentBeadID == "a" && d.Objectives[i].BeadID != "a.0" {
			d.Objectives[i].Status, d.Objectives[i].State = "closed", beads.StateDone
		}
	}
	if !listed() {
		t.Fatal("Confirm done should ask once every sibling is finished")
	}
}
