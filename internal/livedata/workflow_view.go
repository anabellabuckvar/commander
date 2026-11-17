package livedata

import (
	"fmt"
	"sort"
	"strings"
)

// FlowRow is one step as the app's Workflow tab draws it (DerivedDiagram's
// flowOf): its lane, state text, colour, age and whether it is Now.
type FlowRow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Lane   string `json:"lane"` // You, Commander, Agents, Reviewers or Automatic
	Kind   string `json:"kind"`
	Color  string `json:"color"`
	Text   string `json:"text"`
	Age    string `json:"age,omitempty"`
	Now    bool   `json:"now,omitempty"`
	Gate   bool   `json:"gate,omitempty"`
	Inside string `json:"inside,omitempty"` // the open stage it is drawn under
}

var laneLabel = map[string]string{"you": "You", "manager": "Commander", "operators": "Agents", "reviewers": "Reviewers", "automatic": "Automatic"}

// LaneLabel is the column the app draws an actor's steps in.
func LaneLabel(actor string) string {
	if l, ok := laneLabel[actor]; ok {
		return l
	}
	return "Commander"
}

// FlowOf orders d's steps exactly as the app does: done, then Now, then the
// rest happening now, then what is ahead, parked steps last.
func FlowOf(d Diagram, now int64) []FlowRow {
	type item struct {
		s      DiagramStep
		inside string
	}
	var flat []item
	var walk func([]DiagramStep, string)
	walk = func(ss []DiagramStep, inside string) {
		for _, s := range ss {
			if !(len(s.Children) > 0 && s.Kind != "done") {
				flat = append(flat, item{s, inside})
			}
			walk(s.Children, s.Title)
		}
	}
	walk(d.Steps, "")
	parked := func(s DiagramStep) bool { return s.Kind == "idle" && strings.HasPrefix(s.Text, "Parked") }
	live := func(s DiagramStep) bool { return s.Kind != "done" && s.Kind != "idle" }
	find := func(ok func(DiagramStep) bool) *DiagramStep {
		for i := range flat {
			if ok(flat[i].s) {
				return &flat[i].s
			}
		}
		return nil
	}
	nowSet := map[string]bool{}
	for _, x := range flat {
		if x.s.Kind == "needs" {
			nowSet[x.s.ID] = true
		}
	}
	if len(nowSet) == 0 {
		one := find(func(s DiagramStep) bool { return s.ID == d.Current })
		if one == nil || !live(*one) {
			one = find(func(s DiagramStep) bool { return s.Kind == "working" })
		}
		if one == nil {
			one = find(func(s DiagramStep) bool { return s.Kind == "waiting" })
		}
		if one == nil {
			one = find(live)
		}
		if one == nil {
			one = find(func(s DiagramStep) bool { return s.Kind != "done" && !parked(s) })
		}
		if one != nil {
			nowSet[one.ID] = true
		}
	}
	rank := func(s DiagramStep) int {
		switch {
		case s.Kind == "done":
			return 0
		case nowSet[s.ID]:
			return 1
		case live(s):
			return 2
		case parked(s):
			return 4
		}
		return 3
	}
	sort.SliceStable(flat, func(i, j int) bool { return rank(flat[i].s) < rank(flat[j].s) })
	out := make([]FlowRow, 0, len(flat))
	for _, x := range flat {
		out = append(out, FlowRow{ID: x.s.ID, Title: x.s.Title, Lane: LaneLabel(x.s.Actor), Kind: x.s.Kind, Color: StatusColor(x.s.Kind),
			Text: x.s.Text, Age: statusAge(x.s.Since, now), Now: nowSet[x.s.ID], Gate: x.s.Gate, Inside: x.inside})
	}
	return out
}

// WorkflowText is workstream n as its Workflow tab draws it: the row, then
// each level's steps in drawn order, "▶" on Now.
func WorkflowText(r BoardRow, now int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. %s  [%s] %s", r.N, r.Title, StatusColor(r.Kind), r.Text)
	if age := statusAge(r.Since, now); age != "" {
		fmt.Fprintf(&b, " · %s", age)
	}
	b.WriteString("\n")
	if r.Diagram == nil {
		b.WriteString("  (no workflow drawn)\n")
		return b.String()
	}
	level := func(d Diagram, indent string) {
		for _, f := range FlowOf(d, now) {
			mark := " "
			if f.Now {
				mark = "▶"
			}
			gate := ""
			if f.Gate {
				gate = " ◆"
			}
			fmt.Fprintf(&b, "%s%s %-9s %s%s — %s [%s]", indent, mark, f.Lane, f.Title, gate, f.Text, f.Color)
			if f.Age != "" {
				fmt.Fprintf(&b, " · %s", f.Age)
			}
			fmt.Fprintf(&b, " · %s\n", f.ID)
		}
	}
	level(*r.Diagram, "  ")
	for _, t := range r.Diagram.Tracks {
		fmt.Fprintf(&b, "  Track: %s · %s\n", t.Title, t.ID)
		level(t, "    ")
	}
	for _, f := range r.Diagram.Findings {
		fmt.Fprintf(&b, "  ! %s [%s] %s — fix: %s · %s\n", f.Title, f.Check, f.Why, f.Fix, f.Bead)
	}
	return b.String()
}

// FindDiagramStep is the drawn step id in d or any of its tracks, with the
// title of the level it is drawn in.
func FindDiagramStep(d Diagram, id string) (*DiagramStep, string) {
	var walk func([]DiagramStep) *DiagramStep
	walk = func(ss []DiagramStep) *DiagramStep {
		for i := range ss {
			if ss[i].ID == id {
				return &ss[i]
			}
			if s := walk(ss[i].Children); s != nil {
				return s
			}
		}
		return nil
	}
	if s := walk(d.Steps); s != nil {
		return s, d.Title
	}
	for _, t := range d.Tracks {
		if s, at := FindDiagramStep(t, id); s != nil {
			return s, at
		}
	}
	return nil, ""
}
