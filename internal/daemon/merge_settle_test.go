package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/api"
	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
)

func mergeCardSnap(state string) livedata.Snapshot {
	card := livedata.NeedsYou{Project: "p", BeadID: "p-g", Title: "Greenlight merging the Console PR", Workstream: "W", PlanRevision: 11,
		Decision: &store.Decision{Question: "Merge PR 3643 (Console notifications) with admin override?", Options: []store.DecisionOption{
			{Label: "Not yet"}, {Label: "I merged it", Approves: true, Recommended: true}}},
		Basis: &store.Basis{Gen: 1, Raised: 10, PRs: map[string]string{"org/repo#3643": "OPEN"}}}
	return livedata.Snapshot{NeedsYou: []livedata.NeedsYou{card}, PRStates: map[string]string{"org/repo#3643": state},
		Workstreams: []livedata.SnapWorkstream{{Project: "p", Title: "W", ManagerRole: "manager-p-w", PlanningCommander: "commander", Status: "open"}}}
}

func fakeSettler(by string, answers *[][]string, reads *int) *mergeSettler {
	return &mergeSettler{
		viewer: func(context.Context) (string, error) { return "ericelizes", nil },
		readPR: func(_ context.Context, repo, n string) (prMerge, error) {
			*reads++
			if repo != "org/repo" || n != "3643" {
				return prMerge{}, nil
			}
			return prMerge{State: "MERGED", By: by, At: "2026-11-16T17:04:39Z", SHA: "c785f013c2886117d533"}, nil
		},
		answer: func(_ context.Context, argv []string) api.CmdResult {
			*answers = append(*answers, argv)
			return api.CmdResult{OK: true}
		},
	}
}

func TestMergeSettlerAnswersTheCardTheUserMerged(t *testing.T) {
	var answers [][]string
	reads := 0
	m := fakeSettler("ericelizes", &answers, &reads)
	snap := mergeCardSnap("MERGED")
	if got := m.settle(context.Background(), snap); len(got) != 1 {
		t.Fatalf("not settled: %v", got)
	}
	want := "internal-resolve-needs --project p --bead p-g --action decide --choice I merged it --text PR #3643 merged on GitHub by ericelizes at Nov 16 17:04 UTC (c785f013c2) --revision 11"
	if len(answers) != 1 || strings.Join(answers[0], " ") != want {
		t.Fatalf("answer = %q", answers)
	}
	// Idempotent: the next pass neither reads GitHub nor answers again.
	m.settle(context.Background(), snap)
	if len(answers) != 1 || reads != 1 {
		t.Fatalf("repeated: %d answers, %d reads", len(answers), reads)
	}
}

func TestMergeSettlerLeavesSomeoneElsesMergeToTheCommander(t *testing.T) {
	var answers [][]string
	reads := 0
	m := fakeSettler("matt", &answers, &reads)
	snap := mergeCardSnap("MERGED")
	if got := m.settle(context.Background(), snap); len(got) != 0 || len(answers) != 0 {
		t.Fatalf("decided for the user on someone else's merge: %v %v", got, answers)
	}
	m.settle(context.Background(), snap)
	if reads != 1 {
		t.Fatalf("re-read a settled question: %d", reads)
	}
}

func TestMergeSettlerIgnoresOpenPRsAndOtherAsks(t *testing.T) {
	var answers [][]string
	reads := 0
	m := fakeSettler("ericelizes", &answers, &reads)
	m.settle(context.Background(), mergeCardSnap("OPEN"))
	notMerge := mergeCardSnap("MERGED")
	notMerge.NeedsYou[0].Title = "Greenlight shipping"
	notMerge.NeedsYou[0].Decision.Question = "Ship PR 3643 to prod?"
	m.settle(context.Background(), notMerge)
	untracked := mergeCardSnap("MERGED")
	untracked.NeedsYou[0].Decision.Question = "Merge PR 3643 and PR 3700?" // 3700 is not on the board
	m.settle(context.Background(), untracked)
	if len(answers) != 0 || reads != 0 {
		t.Fatalf("answered or read: %v, %d reads", answers, reads)
	}
}

func TestMergeSettlerSkipsAnAnsweredCard(t *testing.T) {
	var answers [][]string
	reads := 0
	m := fakeSettler("ericelizes", &answers, &reads)
	snap := mergeCardSnap("MERGED")
	snap.NeedsYou = nil // answered: the card left NEEDS YOU
	if got := m.settle(context.Background(), snap); len(got) != 0 || len(answers) != 0 || reads != 0 {
		t.Fatal("acted on a card that is gone")
	}
}

func TestPitchWatcherMergeSettleAndHiddenCardMail(t *testing.T) {
	snap := mergeCardSnap("MERGED")
	var to, body []string
	mail := func(_ context.Context, role, _, b string) error {
		to, body = append(to, role), append(body, b)
		return nil
	}
	var answers [][]string
	reads := 0
	w := newPitchWatcher(t.TempDir(), func() (livedata.Snapshot, bool) { return snap, true }, mail)
	w.merges = fakeSettler("ericelizes", &answers, &reads)
	w.pass(context.Background())
	if len(answers) != 1 || len(to) != 0 {
		t.Fatalf("the user's merge: %d answers, mailed %v", len(answers), to)
	}
	w = newPitchWatcher(t.TempDir(), func() (livedata.Snapshot, bool) { return snap, true }, mail)
	w.merges = fakeSettler("matt", &answers, &reads)
	w.pass(context.Background())
	if len(to) != 1 || to[0] != "commander" || !strings.Contains(body[0], "Never tell the user to click this card") {
		t.Fatalf("someone else's merge: mailed %v %q", to, body)
	}
}
