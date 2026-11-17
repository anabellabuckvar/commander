package daemon

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/beads"
)

// Today's real shapes (2026-11-17, vibe-apps): the Notify cutover switch went on at
// 16:05 UTC; a moved notification posted at 18:01:43; prod deploy
// prod-20261117-180317 (run 44143676207) containing b91127b070 succeeded at 18:03.
var (
	cutoverOn    = time.Date(2026, 11, 17, 16, 5, 0, 0, time.UTC)
	reconcileAt  = time.Date(2026, 11, 17, 18, 1, 43, 0, time.UTC)
	deployDoneAt = time.Date(2026, 11, 17, 18, 3, 50, 0, time.UTC)
)

const deployHead = "9f3c1d2e4b5a6978c0d1e2f3a4b5c6d7e8f90123"

func deployGH(t *testing.T, succeeded bool) ghRunner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(cmd, "api repos/acme/web/actions/runs?status=success"):
			runs := `[{"id":44120184444,"name":"Deploy orchestrator to prod","head_sha":"1111111111111111111111111111111111111111","head_branch":"prod-20261117-155709","updated_at":"2026-11-17T16:21:00Z"},
			          {"id":44143000000,"name":"Deploy orchestrator to staging","head_sha":"` + deployHead + `","head_branch":"main","updated_at":"2026-11-17T18:22:00Z"}]`
			if succeeded {
				runs = `[{"id":44143676207,"name":"Deploy orchestrator to prod","head_sha":"` + deployHead + `","head_branch":"prod-20261117-180317","updated_at":"2026-11-17T18:03:50Z"},` + runs[1:]
			}
			return []byte(runs), nil
		case strings.HasPrefix(cmd, "api repos/acme/web/compare/b91127b070..."+deployHead):
			return []byte("ahead\n"), nil
		case strings.HasPrefix(cmd, "api repos/acme/web/compare/b91127b070..."):
			return []byte("behind\n"), nil
		}
		return nil, fmt.Errorf("gh %v", args)
	}
}

func memWatcher(labels []string, now time.Time) (*waitWatcher, *beads.MemClient, *[]sentMail) {
	m := &beads.MemClient{}
	m.Seed(beads.Bead{ID: "p-ws.1", Status: "open", Labels: append([]string{"kind:objective"}, labels...)})
	var sent []sentMail
	w := watcher(&beads.MockClient{}, nil, &sent, now)
	w.beads = m
	w.running = func(context.Context) (map[string]bool, error) { return map[string]bool{}, nil }
	return w, m, &sent
}

func labelsOf(t *testing.T, m *beads.MemClient) []string {
	b, err := m.Show(context.Background(), "/p", "p-ws.1")
	if err != nil || b == nil {
		t.Fatalf("show: %v", err)
	}
	return b.Labels
}

func dispatchReceipts(labels []string) int {
	n := 0
	for _, wt := range beads.WaitsOf(labels) {
		if beads.IsManagerDispatch(wt) {
			n++
		}
	}
	return n
}

// The deploy the steps waited on succeeded at 18:03: within one poll the wait
// clears and, with no Manager and no agent running, the step is handed to
// auto-dispatch (a dispatch receipt), with nobody mailed: not the Commander.
func TestDeployWaitResolvesWithinOnePollAndContinuesWithoutTheCommander(t *testing.T) {
	wt := beads.Wait{Kind: beads.WaitDeploy, Sha: "b91127b070", Since: cutoverOn}
	w, m, sent := memWatcher([]string{wt.Label()}, deployDoneAt.Add(-time.Minute))
	w.managerLive = func(string) bool { return false }
	w.gh = deployGH(t, false)
	w.poll(context.Background())
	if !slices.Contains(labelsOf(t, m), wt.Label()) {
		t.Fatalf("a staging deploy or a prod deploy without the commit resolved the wait")
	}
	w.gh = deployGH(t, true)
	w.now = func() time.Time { return deployDoneAt.Add(time.Minute) }
	w.poll(context.Background())
	got := labelsOf(t, m)
	if slices.Contains(got, wt.Label()) {
		t.Fatalf("deploy wait still holds after the deploy succeeded: %v", got)
	}
	if dispatchReceipts(got) != 1 {
		t.Fatalf("step not handed to auto-dispatch: %v", got)
	}
	if len(*sent) != 0 {
		t.Fatalf("mailed %+v; with no Manager the Commander must not be in the loop", *sent)
	}
}

// The step's own agent is alive and waiting: it is woken with what happened, not duplicated.
func TestResolvedWaitWakesTheStepsOwnRunningAgent(t *testing.T) {
	wt := beads.Wait{Kind: beads.WaitDeploy, Sha: "b91127b070", Since: cutoverOn}
	w, m, sent := memWatcher([]string{wt.Label(), beads.OwnerLabelPrefix + "operator-exec-vm-8-ad0e3b710bc7"}, deployDoneAt.Add(time.Minute))
	w.managerLive = func(string) bool { return false }
	w.running = func(context.Context) (map[string]bool, error) {
		return map[string]bool{"operator-exec-vm-8-ad0e3b710bc7": true}, nil
	}
	w.gh = deployGH(t, true)
	w.poll(context.Background())
	if len(*sent) != 1 || (*sent)[0].to != "operator-exec-vm-8-ad0e3b710bc7" || !strings.HasPrefix((*sent)[0].subject, "[deploy] b91127b is live in prod-20261117-180317") {
		t.Fatalf("mail: %+v", *sent)
	}
	if slices.Contains(labelsOf(t, m), wt.Label()) {
		t.Fatal("wait not cleared")
	}
}

// The moved notification posted at 18:01:43; the next poll resolves the
// wait, reading only messages after the wait began.
func TestSlackEventWaitResolvesOnTheRealReconcileMessage(t *testing.T) {
	wt := beads.Wait{Kind: beads.WaitEvent, Cond: beads.EventSlack, Channel: "C0BFR3WLGEM", Ref: `Fleet reconcile (FAILED|SUCCESS)`, Since: cutoverOn}
	w, m, sent := memWatcher([]string{wt.Label()}, reconcileAt.Add(30*time.Second))
	w.managerLive = func(string) bool { return false }
	var history []slackMessage
	w.slack = func(_ context.Context, channel string, oldest time.Time) ([]slackMessage, error) {
		if channel != "C0BFR3WLGEM" || !oldest.Equal(cutoverOn) {
			return nil, fmt.Errorf("read %s after %v", channel, oldest)
		}
		return history, nil
	}
	w.poll(context.Background())
	if !slices.Contains(labelsOf(t, m), wt.Label()) {
		t.Fatal("resolved before any matching message")
	}
	history = []slackMessage{{TS: "1794938503.000100", Text: ":x: *[Forge PROD] Fleet reconcile FAILED* — Workstations reconcile (run #30225) — 7 ok, 1 failed, 0 unreachable (of 8)"}}
	w.poll(context.Background())
	got := labelsOf(t, m)
	if slices.Contains(got, wt.Label()) || dispatchReceipts(got) != 1 || len(*sent) != 0 {
		t.Fatalf("labels %v, mail %+v", got, *sent)
	}
}

// With no Slack access the wait cannot be watched: its owner hears so once and
// the wait clears, so nothing silently holds the step.
func TestEventWaitWithNoAccessSaysSoOnce(t *testing.T) {
	wt := beads.Wait{Kind: beads.WaitEvent, Cond: beads.EventSlack, Channel: "C0BFR3WLGEM", Ref: `Fleet reconcile`, Since: cutoverOn}
	w, m, sent := memWatcher([]string{wt.Label()}, reconcileAt)
	w.poll(context.Background())
	w.poll(context.Background())
	if len(*sent) != 1 || !strings.Contains((*sent)[0].subject, "cannot watch slack events") {
		t.Fatalf("mail: %+v", *sent)
	}
	if slices.Contains(labelsOf(t, m), wt.Label()) {
		t.Fatal("an unwatchable wait still holds the step")
	}
}

// Past its expect-by time the Commander hears once; the wait still holds.
func TestEventWaitPastItsExpectByTellsTheCommanderOnce(t *testing.T) {
	wt := beads.Wait{Kind: beads.WaitDeploy, Sha: "b91127b070", Since: cutoverOn, By: cutoverOn.Add(time.Hour)}
	w, m, sent := memWatcher([]string{wt.Label()}, cutoverOn.Add(2*time.Hour))
	w.gh = deployGH(t, false)
	w.poll(context.Background())
	w.poll(context.Background())
	if len(*sent) != 1 || (*sent)[0].to != "commander" || !strings.HasPrefix((*sent)[0].subject, "[wait overdue]") {
		t.Fatalf("mail: %+v", *sent)
	}
	if !slices.Contains(labelsOf(t, m), wt.Label()) {
		t.Fatal("an overdue wait must keep holding until it happens or is changed")
	}
}

// PR waits settle on the condition, and wake on the other outcome too.
func TestPRWaitSettles(t *testing.T) {
	for _, c := range []struct{ cond, view, want string }{
		{beads.PRMerged, `{"state":"MERGED","reviewDecision":""}`, "[pr] #3776 merged"},
		{beads.PRApproved, `{"state":"OPEN","reviewDecision":"CHANGES_REQUESTED"}`, "[pr] #3776 changes requested"},
	} {
		wt := beads.Wait{Kind: beads.WaitPR, Cond: c.cond, PR: 3776, Since: cutoverOn}
		w, _, sent := memWatcher([]string{wt.Label()}, reconcileAt)
		w.gh = func(_ context.Context, args ...string) ([]byte, error) {
			if strings.Join(args[:3], " ") == "pr view 3776" {
				return []byte(c.view), nil
			}
			return nil, fmt.Errorf("gh %v", args)
		}
		w.poll(context.Background())
		if len(*sent) != 1 || (*sent)[0].subject != c.want {
			t.Fatalf("%s: mail %+v", c.cond, *sent)
		}
	}
}
