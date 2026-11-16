package livedata

import (
	"strings"
	"testing"

	"github.com/example-user/commander/internal/workflowtemplate"
)

// Track 3.1 on Nov 16: "Admin-merge PR 3772?" was a red card while 3772's
// Integration tests were failing (main-wide, from another PR). A merge card
// reaches the user only when its PR is green and mergeable.
func TestHoldRedMergeCards(t *testing.T) {
	red := workflowtemplate.Head{SHA: "a", State: "OPEN", Mergeable: "MERGEABLE", Checks: []workflowtemplate.Check{
		{Kind: "CheckRun", Name: "Integration tests", Status: "COMPLETED", Conclusion: "FAILURE"},
		{Kind: "CheckRun", Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
	}}
	green := workflowtemplate.Head{SHA: "b", State: "OPEN", Mergeable: "MERGEABLE", Checks: []workflowtemplate.Check{
		{Kind: "CheckRun", Name: "Integration tests", Status: "COMPLETED", Conclusion: "SUCCESS"},
	}}
	heads := map[string]workflowtemplate.Head{
		"https://github.com/acme/app/pull/3772": red,
		"https://github.com/acme/app/pull/3780": green,
	}
	snap := Snapshot{
		Workstreams: []SnapWorkstream{{Project: "app", Repo: "Acme/App"}},
		NeedsYou: []NeedsYou{
			{Project: "app", BeadID: "s1", Title: "Merge PR 3772: fix main's prod-shape gate", Ask: "Admin-merge PR 3772 to unblock prod gate?"},
			{Project: "app", BeadID: "s2", Title: "Merge PR 3780"},
			{Project: "app", BeadID: "s3", Title: "Pick option C for the stranded charts"},
		},
	}
	HoldRedMergeCards(&snap, func(pr string) (workflowtemplate.Head, bool) { h, ok := heads[pr]; return h, ok })
	var ids []string
	for _, n := range snap.NeedsYou {
		ids = append(ids, n.BeadID)
	}
	if strings.Join(ids, ",") != "s2,s3" {
		t.Fatalf("needs you = %v, want the green merge and the non-merge card", ids)
	}
	why := snap.MergeHeld["app\x00s1"]
	if !strings.Contains(why, "PR #3772") || !strings.Contains(why, "Integration tests (failure)") || strings.Contains(why, "Lint") {
		t.Fatalf("held reason = %q", why)
	}
	conflicted := red
	conflicted.Mergeable, conflicted.Checks = "CONFLICTING", green.Checks
	heads["https://github.com/acme/app/pull/3772"] = conflicted
	snap.NeedsYou = append(snap.NeedsYou, NeedsYou{Project: "app", BeadID: "s4", Title: "Merge PR 3772"})
	snap.MergeHeld = nil
	HoldRedMergeCards(&snap, func(pr string) (workflowtemplate.Head, bool) { h, ok := heads[pr]; return h, ok })
	if !strings.Contains(snap.MergeHeld["app\x00s4"], "merge conflicts") {
		t.Fatalf("conflicting PR not held: %v", snap.MergeHeld)
	}
}
