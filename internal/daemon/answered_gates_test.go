package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/store"
)

// Nov 17: three answered gates sat as "Commander: turn your answer into steps".
func TestAnsweredGatesCloseOnTheAnswer(t *testing.T) {
	cases := []struct {
		g    store.AnsweredGate
		want string
	}{
		{store.AnsweredGate{Bead: "zq9h.8.34", Choice: "Drop it"}, "Superseded: the user chose “Drop it”"},
		{store.AnsweredGate{Bead: "596t.13.8", Choice: "I'll tell you where", Text: "forge-bench lives as a Forge app"}, ""},
		// 21:12 Nov 17: these two were closed in error and reopened.
		{store.AnsweredGate{Bead: "6yen.12.3", Choice: "Not now", Text: "then i dont care i dont want to fix that rn"}, ""},
		{store.AnsweredGate{Bead: "596t.11.11", Choice: "Not yet", Text: "Switcher shows the Admin org and app orgs"}, ""},
		{store.AnsweredGate{Bead: "y", Choice: "Not yet", Approved: true, Gate: true}, ""},
		{store.AnsweredGate{Bead: "x", Choice: "Merge it", Approved: true, Gate: true}, "Approved by the user: Merge it"},
		{store.AnsweredGate{Bead: "d624.16.5", Choice: "Wait for the leak fix"}, ""},
	}
	for _, c := range cases {
		if got := gateCloseReason(c.g); got != c.want {
			t.Errorf("%s: %q, want %q", c.g.Bead, got, c.want)
		}
	}
	if !strings.Contains(strings.ToLower(gateCloseReason(cases[0].g)), "superseded") {
		t.Fatal("a dropped gate must read superseded")
	}
}

// vibe-apps-en1h.15.37 (Nov 18): Eric answered "no" / "close it" on a merge card
// and it stayed. A rejecting answer supersedes the gate, closes its PR (his or
// an agent's), and what depended only on it is superseded too.
func TestRejectingAnswerSupersedesTheGate(t *testing.T) {
	for _, said := range []string{"No", "Close it", "close the PR", "Don't merge", "Do not merge it", "Reject", "Drop it"} {
		reason, no := gateSettlement(store.AnsweredGate{Gate: true, Choice: said})
		if !no || !strings.HasPrefix(reason, "Superseded: the user chose “") {
			t.Errorf("%q: %q rejected=%v", said, reason, no)
		}
	}
	for _, said := range []string{"Not now", "No, not yet", "Hold until Friday", "Approve", "Merge it"} {
		if _, no := gateSettlement(store.AnsweredGate{Gate: true, Choice: said, Approved: said == "Approve" || said == "Merge it"}); no {
			t.Errorf("%q read as a rejection", said)
		}
	}
	var closed []string
	var finished []string
	n := closeAnsweredGates(context.Background(), func(context.Context) ([]store.AnsweredGate, error) {
		return []store.AnsweredGate{{Project: "vibe-apps", Bead: "vibe-apps-en1h.15.37", Title: "Merge the fleet-notice Slack fallback PR", Gate: true, Choice: "No"}}, nil
	}, func(_ context.Context, _, bead, reason string) error {
		closed = append(closed, bead+": "+reason)
		return nil
	}, func(_ context.Context, g store.AnsweredGate, reason string) { finished = append(finished, g.Bead) })
	if n != 1 || len(closed) != 1 || !strings.Contains(closed[0], "Superseded: the user chose “No”") || len(finished) != 1 {
		t.Fatalf("closed %v finished %v", closed, finished)
	}
	// The PR: closed when it is the account's own, left open otherwise.
	var calls []string
	gh := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch {
		case args[0] == "pr" && args[1] == "view" && args[2] == "3875":
			return []byte(`{"state":"OPEN","author":{"login":"eric"}}`), nil
		case args[0] == "pr" && args[1] == "view":
			return []byte(`{"state":"OPEN","author":{"login":"someone-else"}}`), nil
		}
		return nil, nil
	}
	login := func(context.Context, ghRunner) string { return "eric" }
	got := closeRejectedPRs(context.Background(), gh, login, []string{"gate", "pr:3875", "pr:other/repo#9"}, "acme/vibe-apps", "vibe-apps-en1h.15.37")
	if len(got) != 1 || got[0] != "acme/vibe-apps#3875" {
		t.Fatalf("closed %v; calls %v", got, calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "close 9") || strings.Contains(c, "comment") {
			t.Fatalf("closed someone else's PR or commented: %v", calls)
		}
	}
}
