package supervision

import (
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/store"
)

// Nov 16: the Commander got "responsibility needs attention" every 2-3 minutes
// and worked one or two items a turn, so 4.2's and 4.4's approved steps sat an
// hour. Its work arrives as one numbered queue, oldest first.
func TestPromptIsOneQueueOldestFirst(t *testing.T) {
	now := time.Unix(100_000, 0)
	item := func(id, title string, opened time.Duration) pending {
		return pending{f: store.SupervisionFact{ProjectID: 2, BeadID: id, Title: title, Epoch: 1, Job: store.SupervisionJob{OpenedAt: now.Add(-opened).Unix(), RequestID: "r-" + id}},
			d: Decision{Description: "Review the finished result"}}
	}
	got := promptAt("commander", []pending{
		item("vibe-apps-zq9h.6.66.24", "Complete speedups", 20*time.Minute),
		item("vibe-apps-zq9h.6.63.14", "Chart open fix A", 70*time.Minute),
		item("vibe-apps-en1h.12.18", "PR 3750 live check", 5*time.Minute),
	}, now)
	if !strings.HasPrefix(got, "Commander queue for commander: 3 things need you, oldest first.") {
		t.Fatalf("header: %q", strings.SplitN(got, "\n", 2)[0])
	}
	first, second, third := strings.Index(got, "1. vibe-apps-zq9h.6.63.14"), strings.Index(got, "2. vibe-apps-zq9h.6.66.24"), strings.Index(got, "3. vibe-apps-en1h.12.18")
	if first < 0 || second < first || third < second {
		t.Fatalf("not oldest first:\n%s", got)
	}
	if !strings.Contains(got, "(waiting 1h10m0s)") || !strings.Contains(got, "--request r-vibe-apps-zq9h.6.63.14") {
		t.Fatalf("missing age or acknowledge command:\n%s", got)
	}
}
