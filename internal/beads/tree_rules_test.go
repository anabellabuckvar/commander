package beads

import (
	"strings"
	"testing"
)

func numbersOf(vs []ShapeViolation) map[int]int {
	out := map[int]int{}
	for _, v := range vs {
		out[v.Number]++
	}
	return out
}

func TestTreeRulesStructureAndOwners(t *testing.T) {
	nodes := []ShapeNode{
		ws("W"),
		track("W.1", "W", "Phase 1"),
		step("W.1.1", "W.1", "Build the cutover", "lane:agents"),
		// rule 3: W holds a workstream and a step
		step("W.2", "W", "Loose step", "lane:agents"),
		// rule 2: an empty workstream
		track("W.3", "W", "Nothing yet"),
		// rule 4: a step with children
		step("W.1.2", "W.1", "Stage", "lane:agents"),
		step("W.1.2.1", "W.1.2", "Under a step", "lane:agents"),
		// rule 6: a workstream with an owner
		{ID: "W.4", Parent: "W", Title: "Owned", Status: "open", Labels: []string{"kind:workstream", "lane:agents"}},
		step("W.4.1", "W.4", "Work", "lane:agents"),
		// rule 7: two owners; rule 8: an unnamed outsider
		step("W.1.3", "W.1", "Two owners", "lane:agents", "lane:you"),
		step("W.1.4", "W.1", "Someone", "lane:elsewhere"),
		// rule 14: your step waiting on an event
		step("W.1.5", "W.1", "Your wait", "lane:you", "waiting:deploy:abc"),
		// rule 18: Confirm done as a step (closing gates are not counted as children)
		step("W.1.6", "W.1", "Confirm done", "gate", "approver:user"),
		// internal and superseded beads are not steps
		step("W.1.1.9", "W.1.1", "Operator task", "kind:task"),
		step("W.1.7", "W.1", "Old", "lane:agents", "superseded"),
	}
	got := numbersOf(TreeRules(nodes))
	for _, n := range []int{2, 3, 4, 6, 7, 8, 14, 18} {
		if got[n] != 1 {
			t.Errorf("rule %d: %d violations, want 1 (all: %v)", n, got[n], got)
		}
	}
	if len(got) != 8 {
		t.Errorf("unexpected rules: %v", got)
	}
}

func TestTreeRulesCleanTree(t *testing.T) {
	nodes := []ShapeNode{
		ws("W"),
		track("W.1", "W", "Phase 1"),
		step("W.1.1", "W.1", "Build the cutover", "lane:agents"),
		step("W.1.2", "W.1", "Review: the cutover", "lane:agents", "review"),
		step("W.1.3", "W.1", "Approve the cutover", "gate", "approver:user"),
		step("W.1.4", "W.1", "Confirm done", "gate", "approver:user", "closing-gate"),
	}
	if vs := TreeRules(nodes); len(vs) != 1 || vs[0].Number != 18 {
		// the closing gate is not a child, but is still a "Confirm done" step (rule 18)
		t.Fatalf("got %+v", vs)
	}
	if vs := TreeRules(nodes[:5]); len(vs) != 0 {
		t.Fatalf("clean tree has violations: %+v", vs)
	}
}

func TestTreeTransitions(t *testing.T) {
	before := []ShapeNode{track("W.1", "W", "Phase 1"), step("W.1.1", "W.1", "Build", "lane:agents"), step("W.1.2", "W.1", "Old way", "lane:agents")}
	closedWS := track("W.1", "W", "Phase 1")
	closedWS.Status = "closed"
	noReason := step("W.1.2", "W.1", "Old way", "lane:agents", "superseded")
	noReason.Status, noReason.CloseReason = "closed", "Superseded:"
	after := []ShapeNode{closedWS, step("W.1.1", "W.1", "Build", "lane:agents"), noReason}
	got := numbersOf(TreeTransitions(before, after))
	if got[26] != 1 || got[27] != 1 {
		t.Fatalf("got %v", got)
	}
	withReason := noReason
	withReason.CloseReason = "Superseded: replaced by W.1.1"
	if vs := TreeTransitions(before, []ShapeNode{before[0], before[1], withReason}); len(vs) != 0 {
		t.Fatalf("a superseded step with a reason refused: %+v", vs)
	}
	if vs := planDeletes([][]string{{"delete", "W.1.2"}}); len(vs) != 1 || vs[0].Number != 27 {
		t.Fatalf("delete not refused: %+v", vs)
	}
}

func TestShapeViolationsCarryRuleNumbers(t *testing.T) {
	vs := numbered([]ShapeViolation{{Rule: "hidden-review", Why: "w", Fix: "f"}, {Rule: "duplicate-step", Why: "w", Fix: "f"}, rule(3, []string{"W"}, "mixed", "split")})
	if vs[0].Number != 13 || vs[1].Number != 0 || vs[2].Number != 3 {
		t.Fatalf("numbers: %+v", vs)
	}
	msg := FormatShapeRefusal(vs)
	for _, want := range []string{"[rule 13] w", "[duplicate-step] w", "[rule 3] mixed", "nothing was written"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, msg)
		}
	}
}

func TestCompileTreeEdits(t *testing.T) {
	ops, e := CompileTreeEdits([]TreeEdit{
		{Op: "create", Parent: "W", Kind: "workstream", Title: "Phase 2", Ref: "p2", Criteria: "Phase 2 notices go through Notify"},
		{Op: "create", Parent: "$p2", Title: "Build phase 2", Owner: "implementer", Ref: "b"},
		{Op: "create", Parent: "$p2", Title: "Review: phase 2", Owner: "reviewer", Ref: "r"},
		{Op: "create", Parent: "$p2", Title: "Merge phase 2", Owner: "you", Ref: "m", Criteria: "The green PR is merged"},
		{Op: "create", Parent: "$p2", Title: "Matt reviews", Owner: "person:Matt"},
		{Op: "link", ID: "$r", After: "$b"},
		{Op: "unlink", ID: "W.1.3", After: "W.1.2"},
		{Op: "retitle", ID: "W.1", Title: "Phase 1"},
		{Op: "set_owner", ID: "W.1.4", Owner: "reviewer"},
		{Op: "set_criteria", ID: "W.1", Criteria: "a\nb"},
		{Op: "move", ID: "W.1.5", Parent: "$p2"},
		{Op: "supersede", ID: "W.1.6", Reason: "replaced by $b"},
		{Op: "close", ID: "W.1.7", Reason: "done in PR 12"},
	})
	if e != nil {
		t.Fatal(e)
	}
	text := make([]string, len(ops))
	for i, op := range ops {
		text[i] = strings.Join(op, " ")
	}
	want := []string{
		"create Phase 2 --parent W --labels kind:workstream --acceptance Phase 2 notices go through Notify --ref p2 --silent",
		"create Build phase 2 --parent $p2 --labels kind:objective,lane:agents --description Build phase 2. --acceptance Final report says what was done, with the evidence (PR, run, output) that it is done. --ref b --silent",
		"lane:agents,review --description Review brief",
		"create Merge phase 2 --parent $p2 --labels kind:objective,gate,approver:user --acceptance The green PR is merged --ref m --silent",
		"--labels kind:objective,lane:elsewhere:Matt --silent",
		"dep add $r $b",
		"dep remove W.1.3 W.1.2",
		"update W.1 --title Phase 1",
		"--remove-label lane:you --remove-label lane:agents",
		"update W.1 --acceptance a\nb",
		"update W.1.5 --parent $p2",
		"update W.1.6 --add-label superseded",
		"close W.1.6 --reason Superseded: replaced by $b",
		"close W.1.7 --reason done in PR 12",
	}
	if len(ops) != len(want) {
		t.Fatalf("got %d ops:\n%s", len(ops), strings.Join(text, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(text[i], w) {
			t.Errorf("op %d = %q, want it to contain %q", i, text[i], w)
		}
	}
	if !strings.HasSuffix(text[8], "--add-label lane:agents --add-label review") {
		t.Errorf("set_owner: %q", text[8])
	}
	for _, bad := range []TreeEdit{
		{Op: "create", Parent: "W", Title: "No owner"},
		{Op: "create", Parent: "W", Title: "Mine", Owner: "commander"},
		{Op: "create", Parent: "W", Kind: "workstream", Title: "Owned", Owner: "you"},
		{Op: "supersede", ID: "X"},
		{Op: "create", Parent: "W", Title: "Approve it", Owner: "you"},
		{Op: "rename", ID: "X"},
		{Op: "link", ID: "X"},
	} {
		if _, e := CompileTreeEdits([]TreeEdit{bad}); e == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, e := CompileTreeEdits([]TreeEdit{{Op: "create", Parent: "W", Title: "Mine", Owner: "commander"}}); e == nil || !strings.Contains(e.Error(), "rule 10") {
		t.Errorf("commander owner: %v", e)
	}
}
