package beads

import "testing"

func reviewed(n ShapeNode, by ...string) ShapeNode { n.Blockers = by; return n }

// 3.2 (vibe-apps-en1h.17) on Nov 17: Eric's merge gate 17.8 waits on the done
// Commander check 17.9 (itself after the agent's PR step 17.3), on 17.3
// directly, and on Eric's own step 15.21 in phase 1's track. Nothing is
// hidden: no review step may be demanded.
func TestHiddenReviewNotWhenAReviewAlreadySitsBetween(t *testing.T) {
	pr := step("W.17.3", "W.17", "Phase 2 PR: get CI green and answer Devin", "runtime:claude")
	pr.Status = "closed"
	check := reviewed(step("W.17.9", "W.17", "Commander checks the phase 2 PR (green, Devin-clean)", "gate", "approver:commander"), "W.17.3")
	check.Status = "closed"
	inbox := step("W.15.21", "W.15", "Check the moved notification in your inbox", "gate", "approver:user")
	gate := reviewed(step("W.17.8", "W.17", "Greenlight: merge phase 2", "gate", "approver:user"), "W.15.21", "W.17.9", "W.17.3")
	nodes := []ShapeNode{ws("W"), track("W.15", "W", "Phase 1"), track("W.17", "W", "Phase 2"), pr, check, inbox, gate}
	if n := rulesOf(ShapeCheck(nodes))["hidden-review"]; n != 0 {
		t.Fatalf("hidden-review fired %d times on a reviewed gate", n)
	}
	// The redundant check the audit's old fix added (17.12) is a duplicate review.
	dup := reviewed(step("W.17.12", "W.17", "Commander checks the result before: Greenlight: merge phase 2", "lane:agents", "review"), "W.17.3")
	if n := rulesOf(ShapeCheck(append(nodes, dup)))["duplicate-review"]; n != 1 {
		t.Fatalf("duplicate review not flagged: %+v", ShapeCheck(append(nodes, dup)))
	}
}

// 6.1 (vibe-apps-596t.13) on Nov 17: "Approve the test cases" followed the
// agent's proposal directly. That is a hidden review; a review step fixes it.
func TestHiddenReviewOnTheProposalAndItsFix(t *testing.T) {
	prop := step("W.13.2", "W.13", "Propose the forge-bench test cases", "lane:agents")
	prop.HasBrief = true
	gate := reviewed(step("W.13.3", "W.13", "Approve the Temporal test cases", "gate", "approver:user"), "W.13.2")
	nodes := []ShapeNode{ws("W"), track("W.13", "W", "Temporal"), prop, gate}
	if rulesOf(ShapeCheck(nodes))["hidden-review"] != 1 {
		t.Fatalf("%+v", ShapeCheck(nodes))
	}
	rev := reviewed(step("W.13.9", "W.13", "Review: the proposed test cases", "lane:agents", "review"), "W.13.2")
	rev.HasBrief = true
	gate.Blockers = []string{"W.13.9"}
	nodes = []ShapeNode{ws("W"), track("W.13", "W", "Temporal"), prop, rev, gate}
	if vs := ShapeCheck(nodes); len(vs) != 0 {
		t.Fatalf("fixed tree flagged: %+v", vs)
	}
}

// The Commander runs no steps; every step has an owner; a review checks work.
func TestOwnerAndReviewRules(t *testing.T) {
	cmd := step("W.1.2", "W.1", "Commander checks the PR", "lane:commander")
	none := step("W.1.3", "W.1", "Do the thing")
	idle := reviewed(step("W.1.4", "W.1", "Review: nothing", "lane:agents", "review"))
	idle.HasBrief = true
	brief := step("W.1.5", "W.1", "Build it", "lane:agents")
	r := rulesOf(ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "T"), cmd, none, idle, brief}))
	if r["commander-step"] != 1 || r["no-owner-tag"] != 1 || r["review-without-work"] != 2 || r["no-brief"] != 1 {
		t.Fatalf("%v", r)
	}
}
