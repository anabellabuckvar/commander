package chat

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Permission prompts are off for the Commander and Operators: cmdrd answers
// each tool prompt itself at once, and refuses only the hard deny-list.
func TestToolPromptsAreAnsweredByCmdrdForOperators(t *testing.T) {
	h := newHarness(t)
	h.opts.AutoAnswer = AutoAnswered
	h.e = New(h.opts)
	h.role("operator-c")
	h.start("operator-c")
	sub := h.subscribe("operator-c")
	ctx := context.Background()

	_ = h.e.Send(ctx, "operator-c", "tool please")
	seen := waitFor(t, sub, isKind(KindResult))
	if !strings.Contains(summarize(seen), "done: hi") {
		t.Fatalf("an allowed Bash call must run without anyone answering: %s", summarize(seen))
	}
	if resp := h.fakeRecord("response-Bash")["response"].(map[string]any); resp["behavior"] != "allow" {
		t.Fatalf("response = %+v", resp)
	}
	if st, _ := h.e.Status("operator-c"); len(st.Pending) != 0 || st.State == "waiting" {
		t.Fatalf("nothing may be left waiting: %+v", st)
	}

	_ = h.e.Send(ctx, "operator-c", "forcepush please")
	seen = waitFor(t, sub, isKind(KindResult))
	if !strings.Contains(summarize(seen), "Bash was denied") {
		t.Fatalf("a force-push must be refused: %s", summarize(seen))
	}
	resp := h.fakeRecord("response-Bash")["response"].(map[string]any)
	if resp["behavior"] != "deny" || !strings.Contains(resp["message"].(string), "force-push") || !strings.Contains(resp["message"].(string), "Commander card") {
		t.Fatalf("refusal = %+v", resp)
	}

	// A question still goes to the user.
	_ = h.e.Send(ctx, "operator-c", "ask me")
	waitFor(t, sub, isKind(KindQuestion))
	if st, _ := h.e.Status("operator-c"); len(st.Pending) != 1 {
		t.Fatalf("a question waits for the user: %+v", st.Pending)
	}
}

// The 2026-11-17 shape: a prompt nobody answers. It is declined after the
// timeout, and the messages queued behind it are then handled.
func TestUnansweredPromptIsDeclinedAndTheQueueMovesOn(t *testing.T) {
	h := newHarness(t)
	h.opts.PromptTimeout = func(string) time.Duration { return 300 * time.Millisecond }
	h.e = New(h.opts)
	h.role("commander")
	h.start("commander")
	sub := h.subscribe("commander")
	ctx := context.Background()

	_ = h.e.Send(ctx, "commander", "tool please")
	waitFor(t, sub, isKind(KindPermission))
	_ = h.e.Send(ctx, "commander", "queued while frozen")
	resolved := waitFor(t, sub, func(e Event) bool { return e.Kind == KindResolved })
	last := resolved[len(resolved)-1].Request
	if last.Decision != "deny" || !strings.Contains(last.Message, "Nobody answered") {
		t.Fatalf("resolved = %+v", last)
	}
	seen := waitFor(t, sub, func(e Event) bool { return e.Kind == KindText && strings.Contains(e.Text, "queued while frozen") })
	if !strings.Contains(summarize(seen), "Bash was denied") || !strings.Contains(summarize(seen), "declined a Bash prompt") {
		t.Fatalf("the declined prompt must end its turn with a notice, then the queue runs: %s", summarize(seen))
	}
	if st, _ := h.e.Status("commander"); len(st.Pending) != 0 {
		t.Fatalf("pending after decline: %+v", st.Pending)
	}
}
