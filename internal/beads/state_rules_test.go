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
	vs := ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Fix"), s})
	if r := rulesOf(vs); r["bad-wait"] != 1 {
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

// 3.4 on Nov 17: the agent's question became "Waiting on you send the alert
// Slack channel ids (Nov 18)": a clock for the user's move, with no card.
func TestStateTimeWaitForTheUser(t *testing.T) {
	w := Wait{Kind: WaitUntil, Since: time.Unix(1_800_000_000, 0), Until: time.Unix(1_800_090_000, 0), Ref: "you send the alert Slack channel ids"}
	s := step("W.1.10", "W.1", "Grafana alerting live", "lane:agents", w.Label())
	if r := rulesOf(ShapeCheck([]ShapeNode{ws("W"), track("W.1", "W", "Alerting"), s})); r["time-wait-for-user"] != 1 {
		t.Fatalf("%v", r)
	}
	if UserTimerLabel("the 6 AM quiet window opens") {
		t.Fatal("a real time flagged")
	}
}
