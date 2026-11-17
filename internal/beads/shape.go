package beads

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Shape checks keep a workstream tree buildable and readable: the Commander's
// plans are refused when they would leave a tree in a shape the board cannot
// show truthfully (duplicate steps, a later stage under way before an earlier
// one starts, the Commander's own check in Eric's column, ...). The rules run
// on a small in-memory model so the same code serves the write guard inside
// the transaction and the read-only `cmdr plan check` report.

// ShapeNode is one bead as the shape rules see it.
type ShapeNode struct {
	ID          string
	Title       string
	Status      string // open | in_progress | deferred | closed
	Labels      []string
	Parent      string
	Blockers    []string // ids this bead depends on (blocks edges)
	Responsible string
	Assigned    string
	HasReason   bool // description or a parked-from label explains a park
}

// ShapeViolation is one broken rule with the beads it names and the fix.
type ShapeViolation struct {
	Rule  string   `json:"rule"`
	Beads []string `json:"beads"`
	Why   string   `json:"why"`
	Fix   string   `json:"fix"`
}

// Key identifies a violation independent of wording, so a write is refused
// only for violations it introduces, not for ones the tree already had.
func (v ShapeViolation) Key() string {
	ids := append([]string(nil), v.Beads...)
	sort.Strings(ids)
	return v.Rule + "|" + strings.Join(ids, ",")
}

func (v ShapeViolation) String() string {
	return fmt.Sprintf("[%s] %s — fix: %s", v.Rule, v.Why, v.Fix)
}

type shapeTree struct {
	byID     map[string]*ShapeNode
	children map[string][]*ShapeNode
}

func newShapeTree(nodes []ShapeNode) shapeTree {
	t := shapeTree{byID: map[string]*ShapeNode{}, children: map[string][]*ShapeNode{}}
	for i := range nodes {
		n := &nodes[i]
		t.byID[n.ID] = n
	}
	for _, n := range t.byID {
		if n.Parent != "" {
			t.children[n.Parent] = append(t.children[n.Parent], n)
		}
	}
	for _, kids := range t.children {
		sort.Slice(kids, func(i, j int) bool { return kids[i].ID < kids[j].ID })
	}
	return t
}

func (n *ShapeNode) has(label string) bool {
	for _, l := range n.Labels {
		if l == label {
			return true
		}
	}
	return false
}
func (n *ShapeNode) prefixed(p string) string {
	for _, l := range n.Labels {
		if v, ok := strings.CutPrefix(l, p); ok {
			return v
		}
	}
	return ""
}
func (n *ShapeNode) closed() bool { return n.Status == "closed" }
func (n *ShapeNode) parked() bool { return n.Status == "deferred" || n.has("parked") }
func (n *ShapeNode) gate() bool   { return n.has("gate") }
func (n *ShapeNode) waiting() bool {
	for _, l := range n.Labels {
		if strings.HasPrefix(l, "w:") {
			return true
		}
	}
	return false
}
func (n *ShapeNode) closing() bool {
	return n.has("closing-gate") || normTitle(n.Title) == "confirm done"
}
func (n *ShapeNode) started() bool {
	return n.Status == "in_progress" || n.closed() || n.waiting() || n.has("cmdr:executing")
}
func (n *ShapeNode) notStarted() bool {
	return n.Status == "open" && !n.waiting() && !n.has("cmdr:executing") && !n.parked()
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func normTitle(s string) string {
	return strings.TrimSpace(nonWord.ReplaceAllString(strings.ToLower(s), " "))
}

// Stages of a delivery workflow, in the order they must happen. 0 = unknown.
const (
	stageSpec = 1 + iota
	stageBuild
	stagePR
	stageCheck
	stageMerge
	stageLive
	stageDone
)

var stageNames = map[int]string{stageSpec: "spec", stageBuild: "build", stagePR: "PR green", stageCheck: "check", stageMerge: "merge greenlight", stageLive: "live", stageDone: "Confirm done"}

func shapeStage(n *ShapeNode) int {
	t := " " + normTitle(n.Title) + " "
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(t, " "+w+" ") {
				return true
			}
		}
		return false
	}
	switch {
	case n.closing():
		return stageDone
	case has("live check", "live", "confirm it s live", "watch"):
		if has("greenlight") && has("merge", "merging") {
			return stageMerge
		}
		return stageLive
	case has("greenlight") && has("merge", "merging", "override merge"):
		return stageMerge
	case strings.HasPrefix(strings.TrimSpace(t), "commander check") || strings.HasPrefix(strings.TrimSpace(t), "commander review") || has("independent review"):
		return stageCheck
	case has("ci green", "get green", "get ci green", "open pr", "answer devin"):
		return stagePR
	case has("implement", "build"):
		return stageBuild
	case has("spec"):
		return stageSpec
	}
	return 0
}

var commanderOwnTitle = regexp.MustCompile(`(?i)^\s*(commander\b|my check\b)`)

func (t shapeTree) isTrack(n *ShapeNode) bool {
	if n.has("kind:track") {
		return true
	}
	// Tracks are the workstream's direct non-gate children (they carry
	// kind:workstream themselves on most boards).
	p := t.byID[n.Parent]
	return p != nil && p.has("kind:workstream") && !n.gate()
}

// ShapeCheck runs every tree rule over the nodes (one or more workstream
// subtrees) and returns the violations in a stable order.
func ShapeCheck(nodes []ShapeNode) []ShapeViolation {
	t := newShapeTree(nodes)
	var out []ShapeViolation
	add := func(v ShapeViolation) { out = append(out, v) }

	parents := make([]string, 0, len(t.children))
	for p := range t.children {
		parents = append(parents, p)
	}
	sort.Strings(parents)
	for _, p := range parents {
		kids := t.children[p]
		// a. duplicates
		seen := map[string]*ShapeNode{}
		for _, k := range kids {
			if k.closed() {
				continue
			}
			key := normTitle(k.Title)
			if key == "" {
				continue
			}
			if prev, ok := seen[key]; ok {
				add(ShapeViolation{Rule: "duplicate-step", Beads: []string{prev.ID, k.ID},
					Why: fmt.Sprintf("%q appears twice under %s (%s and %s)", k.Title, p, prev.ID, k.ID),
					Fix: fmt.Sprintf("keep one; supersede the other (close %s with reason \"superseded by %s\") in this batch", k.ID, prev.ID)})
				continue
			}
			seen[key] = k
		}
		// b. stage order among siblings
		for _, late := range kids {
			ls := shapeStage(late)
			// A closed later step belongs to a finished cycle; only a later
			// step that is working or waiting now can be out of order.
			if ls == 0 || late.closing() || late.parked() || late.closed() || !late.started() {
				continue
			}
			for _, early := range kids {
				es := shapeStage(early)
				if es == 0 || es >= ls || !early.notStarted() {
					continue
				}
				add(ShapeViolation{Rule: "impossible-order", Beads: []string{early.ID, late.ID},
					Why: fmt.Sprintf("%q (%s) is under way while the earlier %s step %q has not started", late.Title, stageNames[ls], stageNames[es], early.Title),
					Fix: fmt.Sprintf("finish or close %s first and make %s wait on it (dep add %s %s); if %s is left from an earlier cycle, close or supersede it; if %s is no longer needed, supersede it", early.ID, late.ID, late.ID, early.ID, late.ID, early.ID)})
			}
		}
		// d. Confirm done asks only once siblings are finished
		for _, c := range kids {
			if !c.closing() || c.closed() || !c.has("needs-user") {
				continue
			}
			var open []string
			for _, s := range kids {
				if s != c && !s.closed() && !s.parked() {
					open = append(open, s.ID)
				}
			}
			if len(open) > 0 {
				add(ShapeViolation{Rule: "early-confirm-done", Beads: append([]string{c.ID}, open...),
					Why: fmt.Sprintf("%s asks Eric to confirm done while %d sibling step(s) are still open", c.ID, len(open)),
					Fix: fmt.Sprintf("remove needs-user from %s until %s are finished", c.ID, strings.Join(open, ", "))})
			}
		}
		// f/g. a merge greenlight sits behind a PR-green or Commander check step
		for _, g := range kids {
			if g.closed() || !g.gate() || shapeStage(g) != stageMerge {
				continue
			}
			var checks []string
			for _, s := range kids {
				if st := shapeStage(s); s != g && (st == stagePR || st == stageCheck) {
					checks = append(checks, s.ID)
				}
			}
			if len(checks) == 0 {
				add(ShapeViolation{Rule: "merge-without-check", Beads: []string{g.ID},
					Why: fmt.Sprintf("merge greenlight %s has no PR-green or Commander check step before it", g.ID),
					Fix: "add a \"get the PR green and answer Devin\" step and a Commander check (lane:commander) before the merge greenlight"})
				continue
			}
			behind := false
			for _, b := range g.Blockers {
				for _, c := range checks {
					if b == c {
						behind = true
					}
				}
			}
			if !behind {
				add(ShapeViolation{Rule: "merge-not-gated", Beads: append([]string{g.ID}, checks...),
					Why: fmt.Sprintf("merge greenlight %s does not wait on its PR-green/check step (%s), so the card can show while the PR is red", g.ID, strings.Join(checks, ", ")),
					Fix: fmt.Sprintf("make the greenlight wait on its check: dep add %s %s", g.ID, checks[len(checks)-1])})
			}
		}
	}
	ids := make([]string, 0, len(t.byID))
	for id := range t.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := t.byID[id]
		if n.closed() {
			continue
		}
		// j. no hidden review: an agent's result reaches the user only through an
		// explicit Commander check step, never through a review nobody can see.
		if n.gate() && n.prefixed("approver:") == "user" && !n.closing() {
			for _, bid := range n.Blockers {
				w, ok := t.byID[bid]
				if !ok || w.gate() || w.Parent != n.Parent {
					continue
				}
				lane := w.prefixed("lane:")
				if lane == "commander" || lane == "you" || strings.HasPrefix(lane, "elsewhere") || commanderOwnTitle.MatchString(w.Title) {
					continue
				}
				add(ShapeViolation{Rule: "hidden-review", Beads: []string{w.ID, n.ID},
					Why: fmt.Sprintf("Eric's gate %q follows the agent step %q directly, so the Commander's review of its result is hidden", n.Title, w.Title),
					Fix: fmt.Sprintf("add a Commander check step (lane:commander) that waits on %s, and make %s wait on that check instead", w.ID, n.ID)})
			}
		}
		// c. the Commander's own steps stay in the Commander column
		if commanderOwnTitle.MatchString(n.Title) {
			lane := n.prefixed("lane:")
			approver := n.prefixed("approver:")
			if lane == "you" || strings.HasPrefix(lane, "elsewhere") || (n.gate() && approver != "" && approver != "commander") {
				add(ShapeViolation{Rule: "commander-lane", Beads: []string{n.ID},
					Why: fmt.Sprintf("%q is the Commander's own step but is placed in %s", n.Title, map[bool]string{true: "Eric's You column", false: "the Reviewers column"}[lane == "you" || approver == "user" || approver == "eric"]),
					Fix: fmt.Sprintf("use lane:commander (or approver:commander for a gate) on %s; \"you\" is Eric's column", n.ID)})
			}
		}
		// e. owner or wait; parked needs a reason
		if n.parked() {
			if !n.HasReason && n.prefixed("parked-from") == "" && n.prefixed("backlog:") == "" {
				add(ShapeViolation{Rule: "parked-without-reason", Beads: []string{n.ID},
					Why: fmt.Sprintf("%q is parked with no reason", n.Title),
					Fix: fmt.Sprintf("give %s a description saying why it is parked, or supersede it if it is stale", n.ID)})
			}
		} else if !n.gate() && n.Responsible == "" && n.Assigned == "" && n.prefixed("owner:") == "" && !n.waiting() && !n.has("kind:workstream") {
			add(ShapeViolation{Rule: "no-owner", Beads: []string{n.ID},
				Why: fmt.Sprintf("%q has no owner and no wait", n.Title),
				Fix: fmt.Sprintf("assign %s (owner or responsible agent) or record what it waits on", n.ID)})
		}
		// h. a live track has real steps, not only Confirm done
		if t.isTrack(n) && !n.parked() {
			kids := t.children[n.ID]
			if len(kids) > 0 {
				only := true
				for _, k := range kids {
					if !k.closing() {
						only = false
						break
					}
				}
				if only {
					add(ShapeViolation{Rule: "empty-track", Beads: []string{n.ID},
						Why: fmt.Sprintf("track %q has only Confirm done and no work or check steps", n.Title),
						Fix: fmt.Sprintf("add its first scoped step under %s, or park it with the reason", n.ID)})
				}
			}
		}
	}
	// b'. dependency cycles and steps started ahead of an unstarted blocker
	state := map[string]int{}
	var cycle []string
	var visit func(id string, path []string) bool
	visit = func(id string, path []string) bool {
		switch state[id] {
		case 1:
			cycle = append(path, id)
			return true
		case 2:
			return false
		}
		state[id] = 1
		if n := t.byID[id]; n != nil {
			for _, b := range n.Blockers {
				if _, ok := t.byID[b]; ok && visit(b, append(path, id)) {
					return true
				}
			}
		}
		state[id] = 2
		return false
	}
	for _, id := range ids {
		if state[id] == 0 && visit(id, nil) {
			add(ShapeViolation{Rule: "dependency-cycle", Beads: cycle,
				Why: "blocked-by chain loops: " + strings.Join(cycle, " → "),
				Fix: "remove one of the dependencies in the loop"})
			break
		}
	}
	return out
}

// shapeAdvisory rules are reported by `cmdr plan check` but never refuse a
// write: parking a step is a normal move and its reason usually lands in a
// comment the shape model does not read.
var shapeAdvisory = map[string]bool{"parked-without-reason": true}

// ShapeNew returns the violations in after that before did not have: a write
// is refused only for problems it introduces.
func ShapeNew(before, after []ShapeViolation) []ShapeViolation {
	had := map[string]bool{}
	for _, v := range before {
		had[v.Key()] = true
	}
	var out []ShapeViolation
	for _, v := range after {
		if !had[v.Key()] && !shapeAdvisory[v.Rule] {
			out = append(out, v)
		}
	}
	return out
}

// ShapeReshape enforces rule i on a batch: when a batch creates two or more
// steps under a parent that already had open steps, every one of those old
// steps must be referenced, closed or superseded in the same batch.
func ShapeReshape(before []ShapeNode, creates map[string]int, referenced map[string]bool) []ShapeViolation {
	t := newShapeTree(before)
	var out []ShapeViolation
	parents := make([]string, 0, len(creates))
	for p, n := range creates {
		if n >= 2 {
			parents = append(parents, p)
		}
	}
	sort.Strings(parents)
	for _, p := range parents {
		var left []string
		for _, k := range t.children[p] {
			if !k.closed() && !referenced[k.ID] && !k.closing() {
				left = append(left, k.ID)
			}
		}
		if len(left) > 0 {
			out = append(out, ShapeViolation{Rule: "reshape-unaccounted", Beads: append([]string{p}, left...),
				Why: fmt.Sprintf("this batch restructures %s but leaves its existing open steps %s untouched", p, strings.Join(left, ", ")),
				Fix: "for each old step: keep it (reference it, e.g. dep a new step on it), close it, or supersede it — in the same batch"})
		}
	}
	return out
}

// FormatShapeRefusal is the error text a refused write returns.
func FormatShapeRefusal(vs []ShapeViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "plan refused: %d tree shape problem(s); nothing was written.\n", len(vs))
	for _, v := range vs {
		b.WriteString("- ")
		b.WriteString(v.String())
		b.WriteString("\n")
	}
	b.WriteString("Fix these in the same batch and resubmit. `cmdr plan check --workstream <id>` lists a tree's problems.")
	return b.String()
}
