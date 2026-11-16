package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/store"
)

func TestContextUseReadsTheLastCallAndTheWindow(t *testing.T) {
	r := &Result{NumTurns: 7,
		Usage:      json.RawMessage(`{"input_tokens":14,"cache_read_input_tokens":815063,"cache_creation_input_tokens":7465,"iterations":[{"input_tokens":2,"cache_read_input_tokens":119829,"cache_creation_input_tokens":315}]}`),
		ModelUsage: json.RawMessage(`{"claude-opus-5-5":{"contextWindow":1000000},"claude-haiku":{"contextWindow":200000}}`)}
	if used, window := contextUse(r); used != 120146 || window != 1_000_000 {
		t.Fatalf("used %d window %d: want the last call (120146) of a 1M window, not the turn's sum", used, window)
	}
	// One call: the top-level usage is that call.
	one := &Result{NumTurns: 1, Usage: json.RawMessage(`{"input_tokens":3,"cache_read_input_tokens":500,"cache_creation_input_tokens":7}`)}
	if used, window := contextUse(one); used != 510 || window != 0 {
		t.Fatalf("used %d window %d", used, window)
	}
	// Several calls and no per-call record: the sum is not the context.
	many := &Result{NumTurns: 3, Usage: json.RawMessage(`{"input_tokens":3,"cache_read_input_tokens":500}`)}
	if used, _ := contextUse(many); used != 0 {
		t.Fatalf("a summed multi-call usage read as the context: %d", used)
	}
	if defaultWindow("opus") != 1_000_000 || defaultWindow("claude-sonnet-5-5[1m]") != 1_000_000 || defaultWindow("haiku") != 200_000 {
		t.Fatal("default windows")
	}
}

func TestCompactDueOnlyForPlanningChatsPastTheThresholdOutsideBackoff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	e := &Engine{opts: Options{Now: func() time.Time { return now }}}
	c := &chatRole{role: "commander", ctxUsed: 760, ctxWindow: 1000}
	if !e.compactDue(c) {
		t.Fatal("76% full Commander is due")
	}
	c.ctxUsed = 740
	if e.compactDue(c) {
		t.Fatal("under 75% is not due")
	}
	c.ctxUsed = 900
	for _, role := range []string{"operator-x", "op-1"} {
		if e.compactDue(&chatRole{role: role, ctxUsed: 900, ctxWindow: 1000}) {
			t.Fatalf("%s compacted early", role)
		}
	}
	if !e.compactDue(&chatRole{role: "manager-w", ctxUsed: 900, ctxWindow: 1000}) {
		t.Fatal("a Manager is due")
	}
	c.compactPending = true
	if e.compactDue(c) {
		t.Fatal("never twice at once")
	}
	c.compactPending, c.compactRetryAt = false, now.Add(time.Minute)
	if e.compactDue(c) {
		t.Fatal("a failed attempt backs off")
	}
	c.compactRetryAt = now
	if !e.compactDue(c) {
		t.Fatal("due again once the back-off is over")
	}
	e.opts.EarlyCompactAt = -1
	if e.compactDue(c) {
		t.Fatal("off")
	}
}

func TestCompactingIsAWorkingTurnWithItsOwnDetail(t *testing.T) {
	tr := newStateTracker()
	tr.observe(Event{Kind: KindSession})
	tr.observe(Event{Kind: KindStatus, Status: "compacting"})
	if s := tr.state(); s.State != store.AgentWorking || s.Detail != CompactingDetail {
		t.Fatalf("compacting reads %+v", s)
	}
	tr.observe(Event{Kind: KindStatus, Status: "idle"})
	tr.observe(Event{Kind: KindCompaction, Compaction: &Compaction{Trigger: "auto"}})
	if s := tr.state(); s.Detail == CompactingDetail {
		t.Fatalf("still compacting after the boundary: %+v", s)
	}
	tr.observe(Event{Kind: KindResult})
	if s := tr.state(); s.State != store.AgentIdle {
		t.Fatalf("idle after the result: %+v", s)
	}
}

func TestIdleManagerPastTheThresholdCompactsEarlyAndSaysSo(t *testing.T) {
	h := newHarness(t)
	h.opts.EarlyCompactDelay = 50 * time.Millisecond
	h.e = New(h.opts)
	h.role("manager-w")
	h.start("manager-w")
	sub := h.subscribe("manager-w")
	// Mail, not the user: a user message holds the compaction while they talk.
	if err := h.e.DeliverMail(context.Background(), "manager-w", "fullctx"); err != nil {
		t.Fatal(err)
	}
	seen := waitFor(t, sub, func(e Event) bool { return e.Kind == KindNotice && e.Subtype == "early_compact" })
	var compacting, boundary bool
	for _, e := range seen {
		compacting = compacting || (e.Kind == KindStatus && e.Status == "compacting") || (e.Kind == KindState && e.State != nil && e.State.Detail == CompactingDetail)
		boundary = boundary || (e.Kind == KindCompaction && e.Compaction.Trigger == "manual")
		if e.Kind == KindUser && strings.Contains(e.Text, "/compact") {
			t.Fatal("the early /compact showed as a user message")
		}
	}
	last := seen[len(seen)-1]
	if !compacting || !boundary || last.Text != "Compacted its memory early (was 80% full, now 10%)" {
		t.Fatalf("compacting %v boundary %v notice %q: %s", compacting, boundary, last.Text, summarize(seen))
	}
}

func TestEarlyCompactionWaitsWhileTheUserTypesAndSkipsOperators(t *testing.T) {
	h := newHarness(t)
	h.opts.EarlyCompactDelay = 50 * time.Millisecond
	h.e = New(h.opts)
	for _, role := range []string{"manager-t", "operator-o"} {
		h.role(role)
		h.start(role)
	}
	subT, subO := h.subscribe("manager-t"), h.subscribe("operator-o")
	_ = h.e.Send(context.Background(), "operator-o", "fullctx")
	_ = h.e.Send(context.Background(), "manager-t", "fullctx") // Send marks the user talking to it
	waitFor(t, subT, isKind(KindResult))
	waitFor(t, subO, isKind(KindResult))
	time.Sleep(400 * time.Millisecond)
	for role, c := range map[string]string{"manager-t": "while the user talks to it", "operator-o": "an Operator"} {
		ch, _, err := h.e.live(role)
		if err != nil {
			t.Fatal(err)
		}
		ch.mu.Lock()
		pending := ch.compactPending
		ch.mu.Unlock()
		if pending {
			t.Fatalf("compacted early %s", c)
		}
	}
}
