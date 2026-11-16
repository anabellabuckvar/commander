package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/store"
)

type fakeCutovers struct {
	all   []store.ExecutionCutover
	steps []string
}

func (f *fakeCutovers) ExecutionCutovers(context.Context) ([]store.ExecutionCutover, error) {
	return f.all, nil
}

func (f *fakeCutovers) RecordCutoverStep(_ context.Context, c store.ExecutionCutover, kind, _ string) error {
	f.steps = append(f.steps, kind)
	for i := range f.all {
		if f.all[i].RunID == c.RunID {
			if kind == store.CutoverToldKind {
				f.all[i].ToldAt = time.Unix(1000, 0).Unix()
			} else {
				f.all[i].ForcedAt = 1
			}
		}
	}
	return nil
}

func TestCutoverTellsTheAgentOnceThenStopsItAfterTheGrace(t *testing.T) {
	f := &fakeCutovers{all: []store.ExecutionCutover{{ProjectID: 1, BeadID: "b", RunID: 7, Agent: "operator-x", To: "claude", From: "codex", Running: true, RequestedAt: 1000}}}
	var sent []string
	var stopped []string
	clock := time.Unix(1000, 0)
	c := executionCutover{store: f,
		send: func(_ context.Context, role, text string) error { sent = append(sent, role+": "+text); return nil },
		stop: func(_ context.Context, role string) error { stopped = append(stopped, role); return nil },
		now:  func() time.Time { return clock },
	}
	c.pass(context.Background())
	c.pass(context.Background())
	if len(sent) != 1 || !strings.Contains(sent[0], "operator-x: [Commander] This step (b) is moving from codex to claude") || !strings.Contains(sent[0], "Final report:") {
		t.Fatalf("told: %q", sent)
	}
	if len(stopped) != 0 {
		t.Fatalf("stopped before the grace: %v", stopped)
	}
	clock = clock.Add(CutoverGrace)
	c.pass(context.Background())
	c.pass(context.Background())
	if len(stopped) != 1 || stopped[0] != "operator-x" {
		t.Fatalf("stopped: %v", stopped)
	}
	if strings.Join(f.steps, ",") != store.CutoverToldKind+","+store.CutoverForcedKind {
		t.Fatalf("recorded: %v", f.steps)
	}
}

func TestCutoverLeavesAQueuedRelaunchAlone(t *testing.T) {
	f := &fakeCutovers{all: []store.ExecutionCutover{{ProjectID: 1, BeadID: "b", RunID: 7, Agent: "operator-x", To: "claude", Running: false}}}
	c := executionCutover{store: f,
		send: func(context.Context, string, string) error { t.Fatal("told a stopped run"); return nil },
		stop: func(context.Context, string) error { t.Fatal("stopped a stopped run"); return nil },
	}
	c.pass(context.Background())
}

func TestStopMessageSaysNobodyContinues(t *testing.T) {
	m := cutoverMessage(store.ExecutionCutover{BeadID: "b", Reason: "Eric closed it"})
	if !strings.Contains(m, "Stop work on b (Eric closed it)") || !strings.Contains(m, "Nobody continues") {
		t.Fatal(m)
	}
}
