package beads

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The board's rules, numbered as on the app's Rules tab. The shape checks in
// shape.go carry their own names; those that are one of these rules are
// reported with its number. TreeRules adds the numbered rules no shape check
// covers. Every bead write, whoever makes it, is refused with the rule's
// number and reason when it makes the tree worse (ShapeNew), and nothing is
// written.
//
//  1-5 structure, 6-13 owners, 14-16 waits and events, 17-20 titles and text,
//  21-27 lifecycle (mostly done by the system), 28-31 content (graded by a
//  model: ContentGrader, not wired yet).

var ruleNumbers = map[string]int{
	"dependency-cycle":           5,
	"no-owner-tag":               7,
	"commander-step":             10,
	"review-without-work":        11,
	"hidden-review":              13,
	"time-wait-for-user":         14,
	"bad-wait":                   15,
	"time-wait-for-event":        16,
	"commander-lane":             20,
	"gate-closed-without-answer": 23,
}

// RuleNumber is a check's number on the Rules tab, 0 when it has none.
func RuleNumber(rule string) int {
	if n, ok := ruleNumbers[rule]; ok {
		return n
	}
	var n int
	if _, e := fmt.Sscanf(rule, "rule-%d", &n); e == nil {
		return n
	}
	return 0
}

// ContentGrader grades a write's titles, criteria and text against rules
// 28-31 (a fast model with a rubric). nil: content is not graded yet. It gets
// the beads the write created or changed, as they are after it, and the tree
// around them; a violation it returns refuses the write like any other.
var ContentGrader func(ctx context.Context, changed []ShapeNode, tree []ShapeNode) []ShapeViolation

func (n *ShapeNode) workstreamBead() bool { return n.has("kind:workstream") || n.has("kind:track") }

// AutomaticLabel is the owner tag of an Automatic step: a Standard with no person
// or agent on it that waits for one event the system sees and closes itself
// when that event happens.
const AutomaticLabel = "lane:automatic"

func (n *ShapeNode) automaticStep() bool { return n.has(AutomaticLabel) }

// WatchableWait: an event cmdrd sees and resolves itself, the one an Automatic step waits for, the one an Automatic step waits for (a deploy, a PR, CI,
// a prod tag, a Slack message, a time). The system's own custody timers are not
// events an Automatic step waits for.
func WatchableWait(w Wait) bool {
	switch w.Kind {
	case WaitDeploy, WaitPR, WaitCI, WaitMainCI, WaitProdTag, WaitEvent:
		return true
	case WaitUntil:
		return !IsManagerHandoff(w) && !IsManagerDispatch(w) && w.Ref != ManagerPlanWait && w.Ref != ManagerStartWait
	}
	return false
}

// watchableWaits are the event waits a bead carries.
func (n *ShapeNode) watchableWaits() []Wait {
	var out []Wait
	for _, l := range n.Labels {
		if w, ok := ParseWaitLabel(l); ok && WatchableWait(w) {
			out = append(out, w)
		}
	}
	return out
}

// internal beads (an agent's task, a gate's record) are not steps in the model.
func (n *ShapeNode) internal() bool {
	return n.has("kind:task") || n.has("kind:gate") || n.has("kind:operational")
}

func (n *ShapeNode) supersededBead() bool {
	return n.has("superseded") || n.has("retired") || n.has("withdrawn") ||
		(n.closed() && strings.Contains(strings.ToLower(n.CloseReason), "superseded"))
}

// laneOwners are the owner tags a step carries (rule 7: exactly one): its lane
// and a user gate's approver. owner:<role> is custody (who holds it right now,
// e.g. the Commander applying an answer), not the step's owner.
func (n *ShapeNode) laneOwners() []string {
	var out []string
	for _, l := range n.Labels {
		if strings.HasPrefix(l, "lane:") {
			out = append(out, l)
		}
	}
	if n.gate() && n.prefixed("approver:") == "user" && !n.has("lane:you") {
		out = append(out, "approver:user")
	}
	return out
}

func rule(n int, ids []string, why, fix string) ShapeViolation {
	return ShapeViolation{Rule: fmt.Sprintf("rule-%d", n), Number: n, Beads: ids, Why: why, Fix: fix}
}

// TreeRules checks the numbered rules about the tree's shape and owners that
// no shape check covers (2, 3, 4, 6, 7, 8, 14, 18).
func TreeRules(nodes []ShapeNode) []ShapeViolation {
	t := newShapeTree(nodes)
	ids := make([]string, 0, len(t.byID))
	for id := range t.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []ShapeViolation
	for _, id := range ids {
		n := t.byID[id]
		if n.internal() || n.supersededBead() {
			continue
		}
		var wsKids, stepKids []string
		for _, k := range t.children[id] {
			if k.internal() || k.supersededBead() || k.closing() {
				continue
			}
			if k.workstreamBead() {
				wsKids = append(wsKids, k.ID)
			} else {
				stepKids = append(stepKids, k.ID)
			}
		}
		if n.workstreamBead() {
			if !n.closed() && len(wsKids)+len(stepKids) == 0 {
				out = append(out, rule(2, []string{id}, fmt.Sprintf("workstream %q has no children", n.Title),
					fmt.Sprintf("create its first step under %s in the same batch, or make it a step", id)))
			}
			if len(wsKids) > 0 && len(stepKids) > 0 {
				out = append(out, rule(3, []string{id}, fmt.Sprintf("workstream %q holds both workstreams (%s) and steps (%s)", n.Title, strings.Join(wsKids, ", "), strings.Join(stepKids, ", ")),
					fmt.Sprintf("move the steps into a nested workstream of %s, or make the nested workstreams steps", id)))
			}
			if o := n.laneOwners(); len(o) > 0 || n.has("needs-user") {
				out = append(out, rule(6, []string{id}, fmt.Sprintf("workstream %q has an owner (%s); its state comes from its children", n.Title, strings.Join(o, ", ")),
					fmt.Sprintf("remove the owner labels from %s and give the work to a step under it", id)))
			}
			if n.waiting() {
				out = append(out, rule(14, []string{id}, fmt.Sprintf("workstream %q waits for an event; only an Automatic step waits", n.Title),
					fmt.Sprintf("put the wait on its own Automatic step under %s, before the steps that need it", id)))
			}
			continue
		}
		if n.automaticStep() {
			if len(wsKids)+len(stepKids) > 0 {
				out = append(out, rule(4, []string{id}, fmt.Sprintf("Automatic step %q has children", n.Title), fmt.Sprintf("move the children of %s out", id)))
			}
			if n.closed() {
				continue
			}
			if ws := n.watchableWaits(); len(ws) != 1 || len(ws) != countWaits(n) {
				out = append(out, rule(14, []string{id}, fmt.Sprintf("Automatic step %q waits for %d events; an Automatic step waits for exactly one the system sees (a deploy, a PR, CI, a Slack message or a time)", n.Title, len(ws)),
					fmt.Sprintf("give %s exactly one wait: deploy:<sha>, pr:<merged|green|approved>:#N, ci:#N, main-ci:<sha>, until:<time> or event:slack:...", id)))
			}
			if o := n.laneOwners(); len(o) > 1 || n.has("needs-user") || n.gate() || n.Assigned != "" {
				out = append(out, rule(7, []string{id}, fmt.Sprintf("Automatic step %q also has a person or agent owner; an Automatic step has no person or agent on it", n.Title),
					fmt.Sprintf("leave %s owned by Automatic only (lane:automatic)", id)))
			}
			continue
		}
		if len(wsKids)+len(stepKids) > 0 {
			out = append(out, rule(4, []string{id}, fmt.Sprintf("step %q has children (%s)", n.Title, strings.Join(append(wsKids, stepKids...), ", ")),
				fmt.Sprintf("make %s a workstream (kind:workstream, no owner) or move its children out", id)))
		}
		if n.closed() {
			continue
		}
		if ws := n.watchableWaits(); len(ws) > 0 {
			out = append(out, rule(14, []string{id}, fmt.Sprintf("step %q waits for %s; only an Automatic step waits", n.Title, ws[0].Text()),
				fmt.Sprintf("add an Automatic step before it: a create edit with \"owner\": \"automatic\" and \"for\": the event, then link %s after it; do not put the wait on %s", id, id)))
		}
		owners := n.laneOwners()
		if len(owners) > 1 {
			out = append(out, rule(7, []string{id}, fmt.Sprintf("step %q has %d owners (%s)", n.Title, len(owners), strings.Join(owners, ", ")),
				fmt.Sprintf("keep one owner on %s: set_owner replaces them all", id)))
		}
		if n.prefixed("lane:") == "elsewhere" {
			out = append(out, rule(8, []string{id}, fmt.Sprintf("step %q is owned by someone outside who is not named", n.Title),
				fmt.Sprintf("name the person or team: owner person:<name> on %s", id)))
		}
		if normTitle(n.Title) == "confirm done" {
			out = append(out, rule(18, []string{id}, fmt.Sprintf("step %s is titled \"Confirm done\"; a workstream is done when its children are", id),
				fmt.Sprintf("drop %s, or title it with the decision or approval itself", id)))
		}
	}
	return out
}

// countWaits counts every wait label a bead carries.
func countWaits(n *ShapeNode) int {
	c := 0
	for _, l := range n.Labels {
		if _, ok := ParseWaitLabel(l); ok {
			c++
		}
	}
	return c
}

// TreeTransitions checks the numbered lifecycle rules a write breaks by what
// it changes (26, 27), comparing the tree before and after it. reasoned are
// the beads the write itself gives a reason for (a --reason, or a comment).
func TreeTransitions(before, after []ShapeNode, reasoned map[string]bool) []ShapeViolation {
	was := map[string]ShapeNode{}
	for _, n := range before {
		was[n.ID] = n
	}
	t := newShapeTree(after)
	var out []ShapeViolation
	for _, n := range after {
		old, existed := was[n.ID]
		if n.workstreamBead() && n.closed() && (!existed || !old.closed()) && !n.supersededBead() {
			var open []string
			for _, k := range t.children[n.ID] {
				if !k.closed() && !k.internal() && !k.supersededBead() && !k.closing() {
					open = append(open, k.ID)
				}
			}
			if len(open) > 0 {
				out = append(out, rule(26, []string{n.ID}, fmt.Sprintf("workstream %q was closed while %s are still open; it is done when all its children are", n.Title, strings.Join(open, ", ")),
					"close or supersede its open children first, in the same batch"))
			}
		}
		// A retirement by the system keeps its reason in its own record.
		if n.supersededBead() && (!existed || !old.supersededBead()) && !n.has("withdrawn") && !n.has("retired") && !reasoned[n.ID] {
			reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(n.CloseReason), "superseded")), ":"))
			if reason == "" {
				out = append(out, rule(27, []string{n.ID}, fmt.Sprintf("%q was superseded with no reason", n.Title),
					fmt.Sprintf("supersede %s with a reason: what replaced it, or why it is no longer needed", n.ID)))
			}
		}
	}
	return out
}

// planDeletes is rule 27's other half: a step or workstream is superseded,
// never deleted. An internal bead (an agent's task, a gate record) may be.
func planDeletes(ops [][]string, before []ShapeNode) []ShapeViolation {
	internal := map[string]bool{}
	for _, n := range before {
		if n.internal() {
			internal[n.ID] = true
		}
	}
	var out []ShapeViolation
	for _, op := range ops {
		if len(op) >= 2 && op[0] == "delete" && !internal[op[1]] {
			out = append(out, rule(27, []string{op[1]}, fmt.Sprintf("%s would be deleted; a replaced bead is superseded, with a reason, never deleted", op[1]),
				fmt.Sprintf("supersede %s instead (edit op supersede, with a reason)", op[1])))
		}
	}
	return out
}

// reasonedBeads are the beads a write gives a reason for: a --reason on an op
// that names it, or a comment added to it.
func reasonedBeads(ops [][]string) map[string]bool {
	out := map[string]bool{}
	for _, op := range ops {
		if len(op) >= 4 && op[0] == "comments" && op[1] == "add" && strings.TrimSpace(op[3]) != "" {
			out[op[2]] = true
			continue
		}
		if len(op) < 2 {
			continue
		}
		pos, f, e := parsePGFlags(op[1:], "--title --description --acceptance --notes --status --priority --type --labels --add-label --remove-label --parent --assignee --reason --force --silent --no-inherit-labels --claim --ref --mode --dry-run --source-project --target-project")
		if e == nil && len(pos) > 0 && strings.TrimSpace(f.one("--reason")) != "" {
			out[pos[0]] = true
		}
	}
	return out
}

// numbered sets each violation's rule number.
func numbered(vs []ShapeViolation) []ShapeViolation {
	for i := range vs {
		if vs[i].Number == 0 {
			vs[i].Number = RuleNumber(vs[i].Rule)
		}
	}
	return vs
}

// changedNodes are the beads in after that the batch created or touched.
func changedNodes(before, after []ShapeNode, touched map[string]bool) []ShapeNode {
	was := map[string]bool{}
	for _, n := range before {
		was[n.ID] = true
	}
	var out []ShapeNode
	for _, n := range after {
		if !was[n.ID] || touched[n.ID] {
			out = append(out, n)
		}
	}
	return out
}
