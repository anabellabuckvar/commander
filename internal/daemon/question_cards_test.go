package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/store"
)

// 3.4 on Nov 17: the Grafana-live agent asked for Slack channel ids and the
// row sat orange with no card. The question becomes Eric's card.
func TestAgentQuestionBecomesACard(t *testing.T) {
	q := store.OpenQuestion{ID: "e7d7", Project: "vibe-apps", Bead: "vibe-apps-596t.12.10", Title: "Grafana alerting live",
		Question: "Need Slack channel IDs (not names) set in the mgmt-notify role, then a deploy", Recommendation: "Use #forge-admin-alerts"}
	var got []string
	n := raiseQuestionCards(context.Background(), func(context.Context) ([]store.OpenQuestion, error) { return []store.OpenQuestion{q}, nil },
		func(_ context.Context, args ...string) error { got = args; return nil })
	if n != 1 || got[0] != "internal-set-objective" || !strings.Contains(strings.Join(got, " "), "--state needs-user") {
		t.Fatalf("%d %v", n, got)
	}
	var d struct {
		Question, Context string
		Input             bool
	}
	if err := json.Unmarshal([]byte(got[len(got)-2]), &d); err != nil || !d.Input || !strings.Contains(d.Question, "Slack channel IDs") || !strings.Contains(d.Context, "#forge-admin-alerts") {
		t.Fatalf("card %+v %v", d, err)
	}
	if answerText("I'll tell you", "C0123 for admin alerts") != "I'll tell you: C0123 for admin alerts" {
		t.Fatal("answer text")
	}
}
