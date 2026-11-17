package livedata

import (
	"strings"
	"testing"
)

// The view orders and marks steps as the app's flowOf does: done, Now (every
// ask of the user), the rest live, what is ahead, parked last.
func TestFlowOfMatchesTheWorkflowTab(t *testing.T) {
	d := Diagram{ID: "w", Current: "w.3", Steps: []DiagramStep{
		{ID: "w.5", Title: "Parked idea", Kind: "idle", Text: "Parked: later", Actor: "operators"},
		{ID: "w.4", Title: "Merge", Kind: "idle", Text: "Not reached", Actor: "you", Gate: true},
		{ID: "w.3", Title: "Build", Kind: "working", Text: "Agent working", Actor: "operators"},
		{ID: "w.1", Title: "Spec", Kind: "done", Text: "Done", Actor: "operators"},
		{ID: "w.2", Title: "Stage", Kind: "working", Text: "In progress", Actor: "manager", Children: []DiagramStep{
			{ID: "w.2.1", Title: "Commander checks", Kind: "waiting", Text: "Commander to check", Actor: "manager"},
		}},
		{ID: "w.6", Title: "Approve cases", Kind: "needs", Text: "Needs you: approve", Actor: "you", Gate: true},
	}}
	got := FlowOf(d, nowSec)
	var ids, lanes []string
	for _, f := range got {
		ids = append(ids, f.ID)
		lanes = append(lanes, f.Lane)
	}
	if strings.Join(ids, " ") != "w.1 w.6 w.3 w.2.1 w.4 w.5" {
		t.Fatalf("order = %v", ids)
	}
	if strings.Join(lanes, " ") != "Agents You Agents Commander You Agents" {
		t.Fatalf("lanes = %v", lanes)
	}
	if !got[1].Now || got[2].Now || got[1].Color != "red" || got[3].Inside != "Stage" {
		t.Fatalf("%+v", got)
	}
	d.Steps[5].Kind = "idle"
	for _, f := range FlowOf(d, nowSec) {
		if f.Now != (f.ID == "w.3") {
			t.Fatalf("with no ask, Now is the current live step: %+v", f)
		}
	}
	text := WorkflowText(BoardRow{N: 3, Title: "Notify", Kind: "working", Text: "Cutover", Diagram: &d}, nowSec)
	if !strings.Contains(text, "▶ Agents    Build — Agent working [green]") || !strings.HasPrefix(text, "3. Notify  [green] Cutover") {
		t.Fatalf("%s", text)
	}
	if s, _ := FindDiagramStep(d, "w.2.1"); s == nil || s.Title != "Commander checks" {
		t.Fatal("nested step not found")
	}
}
