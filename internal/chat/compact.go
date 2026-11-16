package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Early compaction. A Claude session compacts on its own only once its context
// is full, inside the next turn: on a 1M-token Commander that is a minute or
// more of silence while the user waits, and stopping it throws the work away.
// So a Commander or Manager chat past EarlyCompactAt of its window compacts
// while nobody is waiting on it: idle, nothing pending, the user not typing.

const (
	defaultEarlyCompactAt    = 0.75
	defaultEarlyCompactDelay = 45 * time.Second // idle this long after a turn first
	earlyCompactRetry        = 30 * time.Minute // after an attempt that did not compact
)

// contextUse is how full the session's context was at the end of a turn: the
// last API call's prompt (input + cache read + cache write) and the window the
// model reports. Claude's top-level usage sums every call of the turn, so it
// only stands for the context when the turn made one call.
func contextUse(r *Result) (used, window int) {
	if r == nil {
		return 0, 0
	}
	type tokens struct {
		Input       int `json:"input_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
	}
	var u struct {
		tokens
		Iterations []tokens `json:"iterations"`
	}
	_ = json.Unmarshal(r.Usage, &u)
	switch {
	case len(u.Iterations) > 0:
		last := u.Iterations[len(u.Iterations)-1]
		used = last.Input + last.CacheRead + last.CacheCreate
	case r.NumTurns <= 1:
		used = u.Input + u.CacheRead + u.CacheCreate
	}
	var models map[string]struct {
		ContextWindow int `json:"contextWindow"`
	}
	_ = json.Unmarshal(r.ModelUsage, &models)
	for _, m := range models {
		window = max(window, m.ContextWindow)
	}
	return used, window
}

// defaultWindow is the context window when the CLI does not report one.
func defaultWindow(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "[1m]") || strings.Contains(m, "opus") {
		return 1_000_000
	}
	return 200_000
}

func earlyCompactRole(role string) bool {
	return role == "commander" || strings.HasPrefix(role, "manager-")
}

func (e *Engine) earlyCompactAt() float64 {
	if e.opts.EarlyCompactAt == 0 {
		return defaultEarlyCompactAt
	}
	return e.opts.EarlyCompactAt // negative: off
}

// compactDue reports whether c should compact early now. c.mu held.
func (e *Engine) compactDue(c *chatRole) bool {
	at := e.earlyCompactAt()
	return at > 0 && earlyCompactRole(c.role) && !c.compactPending && c.ctxWindow > 0 &&
		float64(c.ctxUsed) >= at*float64(c.ctxWindow) && !e.opts.Now().Before(c.compactRetryAt)
}

// noteContext records how full the session is after a turn and, past the
// threshold, schedules an early compaction for when it is idle.
func (e *Engine) noteContext(c *chatRole, p *process, ev Event) {
	if p.runtime() == RuntimeCodex || ev.Result == nil {
		return
	}
	used, window := contextUse(ev.Result)
	c.mu.Lock()
	if c.proc != p {
		c.mu.Unlock()
		return
	}
	if window == 0 {
		window = defaultWindow(firstNonEmpty(p.info.Model, p.spec.Model))
	}
	if used > 0 {
		c.ctxUsed, c.ctxWindow = used, window
	}
	// The /compact turn ended without a compact boundary: it did not compact.
	failed := c.compactPending
	if failed {
		c.compactPending = false
		c.compactRetryAt = e.opts.Now().Add(earlyCompactRetry)
	}
	due := e.compactDue(c)
	c.mu.Unlock()
	if failed {
		c.publishFrom(p, Event{Kind: KindNotice, Source: SourceEngine, Subtype: "early_compact",
			Text: "Could not compact its memory early; it tries again in 30 minutes"})
	}
	if due {
		e.scheduleCompact(c)
	}
}

func (e *Engine) scheduleCompact(c *chatRole) {
	delay := e.opts.EarlyCompactDelay
	if delay <= 0 {
		delay = defaultEarlyCompactDelay
	}
	c.mu.Lock()
	if c.compactTimer != nil {
		c.compactTimer.Stop()
	}
	c.compactTimer = time.AfterFunc(delay, func() { e.maybeCompact(c.role) })
	c.mu.Unlock()
}

// maybeCompact sends /compact when role is still due and nobody waits on it. A
// chat that is busy again is checked at the end of that turn instead.
func (e *Engine) maybeCompact(role string) {
	c, p, err := e.live(role)
	if err != nil || p.runtime() == RuntimeCodex {
		return
	}
	if e.Talking(role) {
		e.scheduleCompact(c) // the user is typing to it: after they stop
		return
	}
	c.mu.Lock()
	idle := c.proc == p && !c.tracker.turn && len(c.tracker.order) == 0
	if !idle || !e.compactDue(c) {
		c.mu.Unlock()
		return
	}
	c.compactPending = true
	c.compactPct = c.ctxUsed * 100 / c.ctxWindow
	c.mu.Unlock()
	if err := p.write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "/compact"},
		"parent_tool_use_id": nil, "session_id": p.spec.SessionID}); err != nil {
		c.mu.Lock()
		c.compactPending, c.compactRetryAt = false, e.opts.Now().Add(earlyCompactRetry)
		c.mu.Unlock()
		e.opts.Logf("chat: early compaction of %s: %v", role, err)
	}
}

// compacted notes a compact boundary: the context is its new size, and an
// early compaction says so in the chat.
func (e *Engine) compacted(c *chatRole, p *process, ev Event) {
	if ev.Compaction == nil {
		return
	}
	c.mu.Lock()
	early := c.compactPending && ev.Compaction.Trigger == "manual"
	if early {
		c.compactPending = false
	}
	if ev.Compaction.PostTokens > 0 {
		c.ctxUsed = ev.Compaction.PostTokens
	}
	pct, window := c.compactPct, c.ctxWindow
	c.mu.Unlock()
	if !early {
		return
	}
	text := fmt.Sprintf("Compacted its memory early (was %d%% full)", pct)
	if window > 0 && ev.Compaction.PostTokens > 0 {
		text = fmt.Sprintf("Compacted its memory early (was %d%% full, now %d%%)", pct, ev.Compaction.PostTokens*100/window)
	}
	c.publishFrom(p, Event{Kind: KindNotice, Source: SourceEngine, Subtype: "early_compact", Text: text})
}
