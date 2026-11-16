package daemon

import (
	"testing"
	"time"

	"github.com/example-user/commander/internal/linearmove"
	"github.com/example-user/commander/internal/livedata"
)

func TestLinearTargetsFollowTheBoard(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ws := livedata.SnapWorkstream{Project: "p", ProjectID: 1, BeadID: "w", Title: "QA Hub Performance (ENGCHA-5198)", Status: "open", Steps: []livedata.SnapStep{
		{BeadID: "w.1", Title: "Worklists", Track: true, Status: "open", Tickets: []string{"ENGCHA-5308"}, Children: []livedata.SnapStep{
			{BeadID: "w.1.1", Title: "Measure", Status: "closed"},
		}},
		{BeadID: "w.2", Title: "Analytics", Track: true, Status: "open", Tickets: []string{"ENGCHA-5299"}, Children: []livedata.SnapStep{
			{BeadID: "w.2.1", Title: "Profile", Status: "open"},
		}},
		{BeadID: "w.3", Title: "Chart open", Track: true, Status: "closed", Tickets: []string{"ENGCHA-5309"}},
		{BeadID: "w.4", Title: "Caesar route", Track: true, Status: "deferred", Tickets: []string{"ENGCHA-9999"}},
		{BeadID: "w.5", Title: "Old plan", Track: true, Status: "open", Superseded: true, Tickets: []string{"ENGCHA-8888"}},
	}}
	d := &livedata.Display{Workstreams: []livedata.BoardRow{{N: 4, Project: "p", BeadID: "w", Title: ws.Title, Key: "p:w",
		Tracks: []livedata.BoardTrack{{BeadID: "w.1", Letter: ".1", Title: "Worklists"}}}}}
	rows := ticketRows([]livedata.SnapWorkstream{ws}, rowLabels(d), now)
	targets, keep := linearTargets(rows, nil, nil, now)
	check := func(id string, w linearmove.Want) {
		t.Helper()
		if targets[id].Want != w {
			t.Errorf("%s want %v, got %+v", id, w, targets[id])
		}
	}
	check("ENGCHA-5198", linearmove.WantStarted)   // the workstream: work began under it
	check("ENGCHA-5308", linearmove.WantStarted)   // a step done
	check("ENGCHA-5299", linearmove.WantNone)      // nothing begun yet
	check("ENGCHA-5309", linearmove.WantCompleted) // the track was confirmed done
	if _, ok := targets["ENGCHA-9999"]; ok {
		t.Error("a parked track moved its ticket")
	}
	if _, ok := targets["ENGCHA-8888"]; ok {
		t.Error("a superseded track moved its ticket")
	}
	if got := targets["ENGCHA-5308"].Why; got != "work began on #4.1 Worklists" {
		t.Errorf("why = %q", got)
	}
	// The workstream closes and leaves the board: its ticket still goes to Done.
	status := func(pid int64, bead string) (string, bool) { return "closed", pid == 1 && bead == "w" }
	targets, _ = linearTargets(map[string][]rowState{}, keep, status, now.Add(time.Hour))
	if targets["ENGCHA-5198"].Want != linearmove.WantCompleted {
		t.Fatalf("closed workstream's ticket not completed: %+v", targets)
	}
	// A row whose status cannot be read is left alone.
	targets, _ = linearTargets(map[string][]rowState{}, keep, func(int64, string) (string, bool) { return "", false }, now.Add(time.Hour))
	if len(targets) != 0 {
		t.Fatalf("unknown rows moved tickets: %+v", targets)
	}
}

func TestLinearTicketOnTwoRowsIsDoneOnlyWhenBothAre(t *testing.T) {
	open := rowState{ref: ticketCarrierRef{Label: "#1"}, begun: true}
	closed := rowState{ref: ticketCarrierRef{Label: "#1.1"}, closed: true, begun: true}
	if w := want([]rowState{open, closed}); w.Want != linearmove.WantStarted {
		t.Fatalf("got %+v", w)
	}
	if w := want([]rowState{closed, closed}); w.Want != linearmove.WantCompleted {
		t.Fatalf("got %+v", w)
	}
}
