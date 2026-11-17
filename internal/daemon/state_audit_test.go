package daemon

import (
	"context"
	"testing"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/store"
)

// 6.1 on Nov 17: the hidden review is flagged every minute, but the Commander
// hears of it once.
func TestStateAuditMailsOnceAndKeepsTheRow(t *testing.T) {
	var mails []string
	w := &waitWatcher{woken: map[string]bool{},
		mail: func(_ context.Context, to, subject, _ string) error { mails = append(mails, to+": "+subject); return nil },
		shapeReport: func(context.Context, string) ([]beads.ShapeViolation, error) {
			return []beads.ShapeViolation{
				{Rule: "hidden-review", Beads: []string{"vibe-apps-596t.13.2", "vibe-apps-596t.13.3"}, Why: "hidden", Fix: "add a check"},
				{Rule: "parked-without-reason", Beads: []string{"x"}},
			}, nil
		}}
	projects := []store.Project{{Name: "vibe-apps", Path: "/p"}}
	w.auditState(context.Background(), projects)
	w.auditState(context.Background(), projects)
	if len(mails) != 1 || mails[0] != "commander: [invalid state] hidden-review: vibe-apps-596t.13.2, vibe-apps-596t.13.3" {
		t.Fatalf("mails = %v", mails)
	}
	if got := w.InvalidState(); len(got) != 1 || got[0].Rule != "hidden-review" || got[0].Project != "vibe-apps" {
		t.Fatalf("invalid = %+v", got)
	}
}
