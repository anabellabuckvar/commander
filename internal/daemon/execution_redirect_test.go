package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/chat"
	"github.com/example-user/commander/internal/store"
)

type fakeRedirectStore struct {
	changes  []store.PlanChange
	recorded []store.PlanChange
}

func (f *fakeRedirectStore) ChangedExecutionPlans(context.Context) ([]store.PlanChange, error) {
	return f.changes, nil
}

func (f *fakeRedirectStore) RecordExecutionRedirect(_ context.Context, c store.PlanChange, _ string) error {
	f.recorded = append(f.recorded, c)
	return nil
}

func TestExecutionRedirectTellsTheAgentInItsSessionAndRecordsIt(t *testing.T) {
	changed := store.PlanChange{ProjectPath: "/work/app", BeadID: "app-1", Agent: "operator-exec-a", Status: "open", ClaimedRevision: 2, CurrentRevision: 3}
	closed := store.PlanChange{ProjectPath: "/work/app", BeadID: "app-2", Agent: "operator-exec-b", Status: "closed", ClaimedRevision: 4, CurrentRevision: 5}
	fs := &fakeRedirectStore{changes: []store.PlanChange{changed, closed}}
	sent := map[string]string{}
	r := executionRedirector{store: fs, send: func(_ context.Context, role, text string) error {
		sent[role] = text
		return nil
	}}
	r.pass(context.Background())
	if !strings.Contains(sent["operator-exec-a"], "plan revision 2 -> 3") || !strings.Contains(sent["operator-exec-a"], "show app-1") {
		t.Fatalf("changed bead message: %q", sent["operator-exec-a"])
	}
	if !strings.Contains(sent["operator-exec-b"], "is now closed") || !strings.Contains(sent["operator-exec-b"], "Stop this work") {
		t.Fatalf("closed bead message: %q", sent["operator-exec-b"])
	}
	if len(fs.recorded) != 2 {
		t.Fatalf("recorded %d", len(fs.recorded))
	}
}

func TestExecutionRedirectRetriesWhenTheSessionIsUnreachable(t *testing.T) {
	fs := &fakeRedirectStore{changes: []store.PlanChange{{BeadID: "app-1", Agent: "operator-exec-a", Status: "open", ClaimedRevision: 1, CurrentRevision: 2}}}
	r := executionRedirector{store: fs, send: func(context.Context, string, string) error { return errors.New("not running") }}
	r.pass(context.Background())
	if len(fs.recorded) != 0 {
		t.Fatal("an undelivered message was recorded as told")
	}
}

// Nov 17: cmdrd.err logged "execution cutover/redirect: tell
// operator-exec-vm-23-3b53215f3fb8 about vibe-apps-en1h.15.16: chat: no running
// process for this role" every few seconds. A role with no running process is
// retried with backoff, logged once, and given up (recorded as handled).
func TestRedirectToARoleWithNoProcessBacksOffThenGivesUp(t *testing.T) {
	change := store.PlanChange{BeadID: "vibe-apps-en1h.15.16", Agent: "operator-exec-vm-23-3b53215f3fb8", Status: "open", ClaimedRevision: 1, CurrentRevision: 2}
	fs := &fakeRedirectStore{changes: []store.PlanChange{change}}
	sends := 0
	at := time.Unix(1_800_000_000, 0)
	r := executionRedirector{store: fs, dead: newUnreachable(), now: func() time.Time { return at },
		send: func(context.Context, string, string) error { sends++; return chat.ErrNotRunning }}
	for i := 0; i < 30; i++ { // 30 passes 10 s apart: 5 minutes
		r.pass(context.Background())
		at = at.Add(redirectInterval)
	}
	if sends != 2 {
		t.Fatalf("tried %d times in 5 minutes, want 2 (now, then after 1 minute)", sends)
	}
	if len(fs.recorded) != 0 {
		t.Fatal("given up before the last delay")
	}
	at = at.Add(time.Hour)
	for i := 0; i < 4; i++ {
		r.pass(context.Background())
		at = at.Add(30 * time.Minute)
	}
	if len(fs.recorded) != 1 {
		t.Fatalf("recorded %d, want the change marked handled once after the last try", len(fs.recorded))
	}
}

func TestCutoverTellToARoleWithNoProcessBacksOffThenGivesUp(t *testing.T) {
	x := store.ExecutionCutover{BeadID: "vibe-apps-en1h.15.16", Agent: "operator-exec-vm-23-3b53215f3fb8", RunID: 7, Running: true}
	f := &noProcessCutoverStore{all: []store.ExecutionCutover{x}}
	sends := 0
	at := time.Unix(1_800_000_000, 0)
	c := executionCutover{store: f, dead: newUnreachable(), now: func() time.Time { return at },
		send: func(context.Context, string, string) error { sends++; return chat.ErrNotRunning },
		stop: func(context.Context, string) error { return nil }}
	for i := 0; i < 30; i++ {
		c.pass(context.Background())
		at = at.Add(redirectInterval)
	}
	if sends != 2 || len(f.steps) != 0 {
		t.Fatalf("sends %d, steps %v: want 2 tries and nothing recorded yet", sends, f.steps)
	}
	at = at.Add(time.Hour)
	for i := 0; i < 4 && len(f.steps) == 0; i++ {
		c.pass(context.Background())
		at = at.Add(30 * time.Minute)
	}
	if len(f.steps) != 1 || f.steps[0] != store.CutoverToldKind {
		t.Fatalf("steps %v, want one told step recorded as not delivered", f.steps)
	}
}

type noProcessCutoverStore struct {
	all   []store.ExecutionCutover
	steps []string
}

func (f *noProcessCutoverStore) ExecutionCutovers(context.Context) ([]store.ExecutionCutover, error) {
	return f.all, nil
}

func (f *noProcessCutoverStore) RecordCutoverStep(_ context.Context, _ store.ExecutionCutover, kind, _ string) error {
	f.steps = append(f.steps, kind)
	return nil
}
