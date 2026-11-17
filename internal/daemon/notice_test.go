package daemon

import (
	"context"
	"testing"
	"time"
)

// Nov 17: "[health] your plan reads wrong" and "[pitch] write the pitch" went to
// Manager roles that no longer exist and were routed to the Commander. Nothing
// is mailed to the Commander or a Manager now; a sleeping agent is woken in its
// own session.
func TestCmdrdNoticeMailsNoCommanderOrManager(t *testing.T) {
	var sent []string
	n := cmdrdNotice(func(_ context.Context, role, text string) error { sent = append(sent, role); return nil })
	for _, role := range []string{"commander", "manager-vibe-apps-en1h.20", "manager-vibe-apps"} {
		if err := n(context.Background(), role, "[health] your plan reads wrong: fix it now", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := n(context.Background(), "operator-exec-vm-8-ad0e3b710bc7", "[deploy] b91127b0 is live", "x"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "operator-exec-vm-8-ad0e3b710bc7" {
		t.Fatalf("sent to %v", sent)
	}
	if healthLog("plan:manager-x", "detail", time.Now()) != nil {
		t.Fatal("health log failed")
	}
}
