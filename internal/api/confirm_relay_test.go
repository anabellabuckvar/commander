package api

import (
	"context"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/beads"
)

// 4.1 on Nov 17: Eric said in chat "yes move it to done for the worklists"; the
// Commander recorded the approve, and the door then refused applying it, so the
// row sat yellow on the Commander. With the user's answer recorded, the close
// is the user's own and goes through; without one it stays refused.
func TestConfirmDoneClosesOnlyWithTheUsersRecordedAnswer(t *testing.T) {
	gate := &beads.Bead{ID: "vibe-apps-zq9h.6.46", Status: "open", Labels: []string{"kind:objective", "gate", "approver:user", "closing-gate"}}
	bc := &beads.MockClient{ShowResult: gate}
	err := refuseWorkstreamClose(context.Background(), bc, "/p", gate.ID)
	if err == nil || !strings.Contains(err.Error(), "--relayed") {
		t.Fatalf("no recorded answer: err = %v", err)
	}
	gate.HumanAnswerPending = true
	if err := refuseWorkstreamClose(context.Background(), bc, "/p", gate.ID); err != nil {
		t.Fatalf("the user's recorded answer is refused: %v", err)
	}
}
