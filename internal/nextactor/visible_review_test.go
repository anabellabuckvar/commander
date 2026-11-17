package nextactor

import (
	"testing"
	"time"

	"github.com/example-user/commander/internal/beads"
)

// A result waiting on the Commander names its explicit check step.
func TestResultReviewPointsAtTheCommanderCheckStep(t *testing.T) {
	b := Bead{ID: "596t.13.2", Title: "Propose the forge-bench test cases", Status: "open", PlanningCommander: "commander",
		ResultReady: true, ResultSince: now.Add(-time.Minute).Unix(), CheckStep: "Commander checks the Temporal test cases"}
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != Commander || r.Text != "Commander to check “Commander checks the Temporal test cases”" {
		t.Fatalf("%+v", r)
	}
}

// 6.1 at 18:50 Nov 17: a user wait read "Needs you" with no card on Eric's list.
func TestUserWaitWithoutACardIsTheCommandersMove(t *testing.T) {
	w := beads.Wait{Kind: beads.WaitUser, Since: now.Add(-time.Minute)}
	b := Bead{ID: "596t.13.3", Title: "Approve the Temporal test cases", Status: "open", PlanningCommander: "commander",
		Waits: []beads.Wait{w}, UserAsk: "Approve the 6 Temporal test cases", CardsKnown: true}
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != Commander || r.Reason != "card_missing" {
		t.Fatalf("no card listed, still says needs you: %+v", r)
	}
	b.NeedsUser = true
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != User {
		t.Fatalf("listed card: %+v", r)
	}
}
