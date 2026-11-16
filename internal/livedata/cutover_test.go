package livedata

import (
	"testing"

	"github.com/example-user/commander/internal/store"
)

func TestApplyCutoversInheritsRuntimeAndMarksTheSwitch(t *testing.T) {
	snap := Snapshot{Workstreams: []SnapWorkstream{{ProjectID: 1, BeadID: "w", Runtime: "claude", Steps: []SnapStep{
		{BeadID: "w.1", Children: []SnapStep{{BeadID: "w.1.1"}}},
		{BeadID: "w.2", Runtime: "codex", Children: []SnapStep{{BeadID: "w.2.1"}}},
	}}}}
	applyCutovers(&snap, []store.ExecutionCutover{
		{ProjectID: 1, BeadID: "w.1.1", To: "claude", From: "codex", RequestedAt: 10, Running: true},
		{ProjectID: 2, BeadID: "w.2.1", To: "claude", RequestedAt: 10}, // another project
	})
	steps := snap.Workstreams[0].Steps
	if steps[0].Runtime != "claude" || steps[0].Children[0].Runtime != "claude" {
		t.Fatalf("not inherited: %+v", steps[0])
	}
	if steps[1].Runtime != "codex" || steps[1].Children[0].Runtime != "codex" {
		t.Fatalf("own label lost: %+v", steps[1])
	}
	if c := steps[0].Children[0].Cutover; c == nil || c.To != "claude" || c.From != "codex" || !c.Running || c.Since != 10 {
		t.Fatalf("cutover: %+v", c)
	}
	if steps[1].Children[0].Cutover != nil {
		t.Fatal("a cutover in another project marked this step")
	}
}
