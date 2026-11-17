package beads

import (
	"strings"
	"testing"
	"time"
)

// Nov 17: an until wait stood in for the prod deploy, so the step sat after it shipped.
func TestStateTimeWaitForADeploy(t *testing.T) {
	w := Wait{Kind: WaitUntil, Since: time.Unix(1_800_000_000, 0), Until: time.Unix(1_800_003_600, 0), Ref: "after the prod deploy lands"}
	s := step("W.1.4", "W.1", "Confirm the fix live", w.Label())
	vs := ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Fix"), s})
	if rulesOf(vs)["time-wait-for-event"] != 1 || !strings.Contains(vs[0].Fix, "deploy:<sha>") {
		t.Fatalf("%+v", vs)
	}
	ok, err := ParseWait("deploy:b91127b070")
	if err != nil {
		t.Fatal(err)
	}
	ok.Since = w.Since
	s = step("W.1.4", "W.1", "Confirm the fix live", ok.Label())
	if vs := ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Fix"), s}); len(vs) != 0 {
		t.Fatalf("deploy wait flagged: %+v", vs)
	}
}

func TestStateBadWaitAndStaleAnswer(t *testing.T) {
	s := step("W.1.4", "W.1", "Watch", "waiting:notanumber:until")
	g := step("W.1.5", "W.1", "Approve", "gate", "approver:user")
	g.Status, g.AnswerPending = "closed", true
	vs := ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Fix"), s, g})
	if r := rulesOf(vs); r["bad-wait"] != 1 || r["stale-answer"] != 1 {
		t.Fatalf("%+v", vs)
	}
}

// 4.1 on Nov 17: only the user's answer closes their gate; an agent closing it
// without one is refused, and with a recorded (or relayed) answer it applies.
func TestStateAgentClosingTheUsersGate(t *testing.T) {
	before := step("W.1.9", "W.1", "Confirm done", "gate", "approver:user", "closing-gate")
	after := before
	after.Status = "closed"
	if vs := ShapeTransitions([]ShapeNode{before}, []ShapeNode{after}, true); rulesOf(vs)["gate-closed-without-answer"] != 1 || !strings.Contains(vs[0].Fix, "--relayed") {
		t.Fatalf("%+v", vs)
	}
	before.AnswerPending = true
	if vs := ShapeTransitions([]ShapeNode{before}, []ShapeNode{after}, true); len(vs) != 0 {
		t.Fatalf("recorded answer refused: %+v", vs)
	}
	before.AnswerPending = false
	if vs := ShapeTransitions([]ShapeNode{before}, []ShapeNode{after}, false); len(vs) != 0 {
		t.Fatalf("the user's own click refused: %+v", vs)
	}
}
