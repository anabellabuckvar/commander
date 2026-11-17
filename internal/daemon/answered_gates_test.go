package daemon

import (
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
		{store.AnsweredGate{Bead: "596t.13.8", Choice: "I'll tell you where", Text: "forge-bench lives as a Forge app"}, "Answered by the user: I'll tell you where: forge-bench lives as a Forge app"},
		{store.AnsweredGate{Bead: "x", Choice: "Merge it", Approved: true}, "Approved by the user: Merge it"},
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
