package beads

import (
	"context"
	"strings"
	"testing"
	"time"
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
		// rule 14: a step that waits (only an Automatic step waits)
		step("W.1.5", "W.1", "Your wait", "lane:you", deployLabel()),
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
	got := numbersOf(TreeTransitions(before, after, nil))
	if got[26] != 1 || got[27] != 1 {
		t.Fatalf("got %v", got)
	}
	withReason := noReason
	withReason.CloseReason = "Superseded: replaced by W.1.1"
	if vs := TreeTransitions(before, []ShapeNode{before[0], before[1], withReason}, nil); len(vs) != 0 {
		t.Fatalf("a superseded step with a reason refused: %+v", vs)
	}
	if vs := TreeTransitions(before, after[1:], reasonedBeads([][]string{{"update", "W.1.2", "--add-label", "superseded"}, {"comments", "add", "W.1.2", "replaced by the new cutover"}})); len(vs) != 0 {
		t.Fatalf("a supersede whose reason rides in the same write refused: %+v", vs)
	}
	if vs := planDeletes([][]string{{"delete", "W.1.2"}}, before); len(vs) != 1 || vs[0].Number != 27 {
		t.Fatalf("delete not refused: %+v", vs)
	}
	if vs := planDeletes([][]string{{"delete", "T"}}, []ShapeNode{step("T", "W.1.1", "operator task", "kind:task")}); len(vs) != 0 {
		t.Fatalf("an internal bead's delete refused: %+v", vs)
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
	top, e := CompileTreeEdits([]TreeEdit{{Op: "create", Kind: "workstream", Title: "Release 3", Ref: "r3"}, {Op: "create", Parent: "$r3", Title: "Plan release 3", Owner: "implementer"}})
	if e != nil || strings.Join(top[0], " ") != "create Release 3 --labels kind:workstream --ref r3 --silent" || top[1][3] != "$r3" {
		t.Fatalf("top-level workstream: %v %v", top, e)
	}
	if _, e := CompileTreeEdits([]TreeEdit{{Op: "create", Title: "Loose step", Owner: "you", Criteria: "x"}}); e == nil {
		t.Fatal("a step with no parent was accepted")
	}
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

// Every writer, not only a plan, is held to the numbered rules with no
// worsening: a new break is refused, an old one left as it was is not.
func TestPostgresEveryWriteRefusesOnlyNewBreaks(t *testing.T) {
	c, dir, _ := postgresBeads(t)
	t.Setenv("CMDR_SHAPE_CHECK", "0") // the fixture: an empty workstream alone breaks rule 2
	ctx := context.Background()
	if e := c.Init(ctx, dir); e != nil {
		t.Fatal(e)
	}
	mk := func(title, parent string, labels ...string) string {
		t.Helper()
		id, e := c.Create(ctx, dir, title, CreateOptions{Parent: parent, Labels: labels, Silent: true})
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	brief := []string{"--description", "Do it.", "--acceptance", "It is done."}
	root := mk("Workstream", "", "kind:workstream")
	nested := mk("Nested", root, "kind:workstream")
	t.Setenv("CMDR_SHAPE_CHECK", "1")
	if resp := postgresApplyOps(ctx, dir, [][]string{append([]string{"create", "Build it", "--parent", nested, "--labels", "kind:objective,lane:agents", "--assignee", "operator-x", "--silent"}, brief...)}); resp.Exit != 0 {
		t.Fatalf("valid step refused: %+v", resp)
	}
	// A step straight under a workstream that holds a workstream: a new break of rule 3.
	resp := postgresApplyOps(ctx, dir, [][]string{append([]string{"create", "Loose step", "--parent", root, "--labels", "kind:objective,lane:agents", "--assignee", "operator-x", "--silent"}, brief...)})
	if resp.Exit == 0 || !strings.Contains(resp.Error, "[rule 3]") {
		t.Fatalf("a raw write that mixes a workstream was not refused with rule 3: %+v", resp)
	}
	// A tree that already breaks rule 3 (written with the check off) still takes
	// writes that leave the break as it was.
	t.Setenv("CMDR_SHAPE_CHECK", "0")
	loose := mk("Loose step", root, "kind:objective", "lane:agents")
	t.Setenv("CMDR_SHAPE_CHECK", "1")
	if resp := postgresApplyOps(ctx, dir, [][]string{{"update", loose, "--title", "Loose step, renamed"}}); resp.Exit != 0 {
		t.Fatalf("a write that leaves an old break unchanged was refused: %+v", resp)
	}
	// ...but not one that adds a second break to the same bead (rule 7: two owners).
	if resp := postgresApplyOps(ctx, dir, [][]string{{"update", loose, "--add-label", "lane:you"}}); resp.Exit == 0 || !strings.Contains(resp.Error, "[rule 7]") {
		t.Fatalf("a new break on an already broken bead was not refused: %+v", resp)
	}
}

func TestAutomaticSteps(t *testing.T) {
	deploy, _ := ParseWait("deploy:53c4be7949")
	deploy.Since = time.Unix(1_800_000_000, 0)
	pr, _ := ParseWait("pr:merged:#12")
	pr.Since = deploy.Since
	event := func(id string, labels ...string) ShapeNode {
		return ShapeNode{ID: id, Parent: "W.1", Title: "Waiting for a deploy", Status: "open", Labels: append([]string{"kind:objective", AutomaticLabel}, labels...)}
	}
	nodes := []ShapeNode{
		ws("W"), track("W.1", "W", "Phase 1"),
		event("W.1.1", deploy.Label()),                                       // good
		event("W.1.2"),                                                       // names no event
		event("W.1.3", deploy.Label(), pr.Label()),                           // names two
		event("W.1.4", deploy.Label(), "lane:agents"),                        // has an owner
		step("W.1.5", "W.1", "Check it live", "lane:agents", deploy.Label()), // a step that waits
	}
	nodes[6].Blockers = []string{"W.1.1"}
	got := map[string][]int{}
	for _, v := range TreeRules(nodes) {
		got[v.Beads[0]] = append(got[v.Beads[0]], v.Number)
	}
	if len(got["W.1.1"]) != 0 || got["W.1.2"][0] != 14 || got["W.1.3"][0] != 14 || got["W.1.4"][0] != 7 || got["W.1.5"][0] != 14 {
		t.Fatalf("event rules: %v", got)
	}
	// An Automatic step is not agent work: it needs no owner tag, brief or review.
	shape := ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Phase 1"), event("W.1.1", deploy.Label()), step("W.1.2", "W.1", "Merge it", "gate", "approver:user")})
	for _, v := range shape {
		if v.Beads[0] == "W.1.1" {
			t.Fatalf("shape check on an Automatic step: %+v", v)
		}
	}
	ops, e := CompileTreeEdits([]TreeEdit{{Op: "create", Parent: "W.1", Owner: "automatic", For: "deploy:53c4be7949", Ref: "deployed"}, {Op: "link", ID: "W.1.5", After: "$deployed"}})
	if e != nil || len(ops) != 3 || ops[0][1] != "Waiting for "+deploy.Text() || !strings.Contains(strings.Join(ops[0], " "), AutomaticLabel) || ops[1][0] != "update" || !strings.HasPrefix(ops[1][3], WaitLabelPrefix) {
		t.Fatalf("event edit: %v %v", ops, e)
	}
	for _, bad := range []TreeEdit{{Op: "create", Parent: "W.1", Owner: "automatic"}, {Op: "create", Parent: "W.1", Owner: "automatic", For: "user"}, {Op: "create", Parent: "W.1", Title: "Build", Owner: "implementer", For: "ci:#3"}} {
		if _, e := CompileTreeEdits([]TreeEdit{bad}); e == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// Migration: the step's wait moves onto an Automatic step before it.
	mig := AutomaticStepOps(Bead{ID: "W.1.5", Parent: "W.1", Labels: []string{"lane:agents", deploy.Label()}})
	text := make([]string, len(mig))
	for i, op := range mig {
		text[i] = strings.Join(op, " ")
	}
	want := []string{"create Waiting for " + deploy.Text() + " --labels kind:objective,lane:automatic --ref autoW_1_5_1 --silent --parent W.1", "update $autoW_1_5_1 --add-label " + deploy.Label(), "dep add W.1.5 $autoW_1_5_1", "update W.1.5 --remove-label " + deploy.Label()}
	if strings.Join(text, "\n") != strings.Join(want, "\n") {
		t.Fatalf("migration ops:\n%s", strings.Join(text, "\n"))
	}
}

func deployLabel() string {
	w, _ := ParseWait("deploy:abc1234")
	w.Since = time.Unix(1_800_000_000, 0)
	return w.Label()
}

// vibe-apps-en1h.15.37 (Nov 18): applying Eric's "no" to a merge card adds the
// Commander's custody (owner:commander) beside the gate's approver; that is not
// a second owner.
func TestCustodyIsNotASecondOwner(t *testing.T) {
	gate := step("W.1.1", "W.1", "Merge the fleet-notice Slack fallback PR", "gate", "approver:user", "owner:commander", "decision:answered")
	for _, v := range TreeRules([]ShapeNode{ws("W"), track("W.1", "W", "Phase"), gate}) {
		if v.Number == 7 {
			t.Fatalf("custody read as an owner: %+v", v)
		}
	}
}
