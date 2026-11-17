package nextactor

import (
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/beads"
)

// OM Hub on Nov 17: the inventory's agent finished with a clean final report.
// The step closes itself; no Commander review is ever its state.
func TestOMHubInventoryClosesItselfNoCommander(t *testing.T) {
	b := Bead{ID: "bead-d624b82819c6.1", Title: "Inventory every OM Hub background job (trigger, runtime, failure behaviour)", Status: "open",
		PlanningCommander: "commander", ResultReady: true, ResultSince: now.Add(-time.Minute).Unix(), ResultClean: true, ResultLine: "Final report: inventory written"}
	r := Decide(b, ctxWith(false, 0, nil))
	if r.Actor != Agent || r.Reason != "result_accepting" || strings.Contains(r.Text, "Commander") {
		t.Fatalf("%+v", r)
	}
	// The next step, an agent step with nothing holding it, is queued for an agent.
	next := Bead{ID: "bead-d624b82819c6.2", Title: "Write the OM Hub cutover plan", Status: "open", PlanningCommander: "commander"}
	if r := Decide(next, ctxWith(false, 0, nil)); r.Actor != Agent || r.Reason != "ready" {
		t.Fatalf("next step: %+v", r)
	}
	// A blocked report stops the flow and says why, without the Commander.
	b.ResultClean, b.ResultLine = false, "Final report: BLOCKED on prod access"
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != Stuck || !strings.Contains(r.Text, "BLOCKED on prod access") {
		t.Fatalf("blocked: %+v", r)
	}
}

// 6.1 at 18:50 Nov 17: a user wait read "Needs you" with no card on Eric's list.
func TestUserWaitWithoutACardIsInvalid(t *testing.T) {
	w := beads.Wait{Kind: beads.WaitUser, Since: now.Add(-time.Minute)}
	b := Bead{ID: "596t.13.3", Title: "Approve the Temporal test cases", Status: "open", PlanningCommander: "commander",
		Waits: []beads.Wait{w}, UserAsk: "Approve the 6 Temporal test cases", CardsKnown: true}
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != Stuck || r.Reason != "invalid:no-card" {
		t.Fatalf("no card listed, still says needs you: %+v", r)
	}
	b.NeedsUser = true
	if r := Decide(b, ctxWith(false, 0, nil)); r.Actor != User {
		t.Fatalf("listed card: %+v", r)
	}
}
