package beads

import (
	"errors"
	"fmt"
	"strings"
)

// TreeEdit is one edit of the workstream tree in the board's own words. A
// plan's edits compile to its write operations and apply as one atomic batch
// with any ops beside them: all of it is written, or (refused with the rule's
// number and reason) none of it.
type TreeEdit struct {
	// Op is create, retitle, set_owner, set_criteria, move, link, unlink, supersede or close.
	Op string `json:"op"`
	// ID is the bead edited, or "$<ref>" for one created earlier in the batch.
	ID string `json:"id,omitempty"`
	// Ref names a created bead for later edits ("$<ref>").
	Ref string `json:"ref,omitempty"`
	// Parent: create and move: the workstream it goes under.
	Parent string `json:"parent,omitempty"`
	// Kind: create: workstream or step (default step).
	Kind  string `json:"kind,omitempty"`
	Title string `json:"title,omitempty"`
	// Owner: a step's one owner: you, implementer, reviewer or person:<name or team>.
	Owner string `json:"owner,omitempty"`
	// Criteria: what must be true for it to count as done (one per line).
	Criteria string `json:"criteria,omitempty"`
	// Brief: create: what an agent owner is to do (its description).
	Brief string `json:"brief,omitempty"`
	// After: link and unlink: the step or workstream ID waits on.
	After string `json:"after,omitempty"`
	// Reason: supersede and close.
	Reason string `json:"reason,omitempty"`
}

// ownerLabels are the labels that make owner a step's one owner.
func ownerLabels(owner string) (string, error) {
	switch o := strings.TrimSpace(owner); {
	case o == "you":
		return "gate,approver:user", nil
	case o == "implementer":
		return "lane:agents", nil
	case o == "reviewer":
		return "lane:agents,review", nil
	case strings.HasPrefix(o, "person:") && strings.TrimSpace(strings.TrimPrefix(o, "person:")) != "":
		who := strings.TrimSpace(strings.TrimPrefix(o, "person:"))
		if strings.ContainsAny(who, ",\n") {
			return "", fmt.Errorf("owner %q: a name cannot hold a comma", owner)
		}
		return "lane:elsewhere:" + who, nil
	case o == "commander" || o == "manager":
		return "", errors.New("rule 10: nothing is owned by the Commander; owner is you, implementer, reviewer or person:<name>")
	}
	return "", fmt.Errorf("owner %q: use you, implementer, reviewer or person:<name> (rules 7-9)", owner)
}

// ownerTags are every owner label set_owner replaces.
var ownerTags = []string{"lane:you", "lane:agents", "lane:commander", "lane:elsewhere", "review", "gate", "approver:user", "needs-user"}

// CompileTreeEdits turns edits into the plan's write operations, in order.
func CompileTreeEdits(edits []TreeEdit) ([][]string, error) {
	var ops [][]string
	for i, e := range edits {
		bad := func(format string, a ...any) error {
			return fmt.Errorf("edit %d (%s): %s", i+1, e.Op, fmt.Sprintf(format, a...))
		}
		need := func(fields map[string]string) error {
			for name, v := range fields {
				if strings.TrimSpace(v) == "" {
					return bad("%s is required", name)
				}
			}
			return nil
		}
		switch e.Op {
		case "create":
			if err := need(map[string]string{"parent": e.Parent, "title": e.Title}); err != nil {
				return nil, err
			}
			op := []string{"create", e.Title, "--parent", e.Parent}
			switch e.Kind {
			case "workstream":
				if e.Owner != "" {
					return nil, bad("rule 6: a workstream has no owner")
				}
				op = append(op, "--labels", "kind:workstream")
				if e.Criteria != "" {
					op = append(op, "--acceptance", e.Criteria)
				}
				if e.Brief != "" {
					op = append(op, "--description", e.Brief)
				}
			case "", "step":
				if e.Owner == "" {
					return nil, bad("rule 7: a step has exactly one owner: you, implementer, reviewer or person:<name>")
				}
				labels, err := ownerLabels(e.Owner)
				if err != nil {
					return nil, bad("%v", err)
				}
				if e.Owner == "you" && strings.TrimSpace(e.Criteria) == "" {
					return nil, bad("your decision needs criteria: what counts as passed (the card shows them)")
				}
				op = append(op, "--labels", "kind:objective,"+labels)
				desc, acc := e.Brief, e.Criteria
				if strings.Contains(labels, "lane:agents") {
					d, a := stepBrief(planStep{labels: labels}, e.Title)
					if desc == "" {
						desc = d
					}
					if acc == "" {
						acc = a
					}
				}
				if desc != "" {
					op = append(op, "--description", desc)
				}
				if acc != "" {
					op = append(op, "--acceptance", acc)
				}
			default:
				return nil, bad("kind is workstream or step")
			}
			if e.Ref != "" {
				op = append(op, "--ref", e.Ref)
			}
			ops = append(ops, append(op, "--silent"))
		case "retitle":
			if err := need(map[string]string{"id": e.ID, "title": e.Title}); err != nil {
				return nil, err
			}
			ops = append(ops, []string{"update", e.ID, "--title", e.Title})
		case "set_owner":
			if err := need(map[string]string{"id": e.ID, "owner": e.Owner}); err != nil {
				return nil, err
			}
			labels, err := ownerLabels(e.Owner)
			if err != nil {
				return nil, bad("%v", err)
			}
			op := []string{"update", e.ID}
			for _, l := range ownerTags {
				op = append(op, "--remove-label", l)
			}
			for _, l := range strings.Split(labels, ",") {
				op = append(op, "--add-label", l)
			}
			ops = append(ops, op)
		case "set_criteria":
			if err := need(map[string]string{"id": e.ID, "criteria": e.Criteria}); err != nil {
				return nil, err
			}
			ops = append(ops, []string{"update", e.ID, "--acceptance", e.Criteria})
		case "move":
			if err := need(map[string]string{"id": e.ID, "parent": e.Parent}); err != nil {
				return nil, err
			}
			ops = append(ops, []string{"update", e.ID, "--parent", e.Parent})
		case "link", "unlink":
			if err := need(map[string]string{"id": e.ID, "after": e.After}); err != nil {
				return nil, err
			}
			sub := "add"
			if e.Op == "unlink" {
				sub = "remove"
			}
			ops = append(ops, []string{"dep", sub, e.ID, e.After})
		case "supersede":
			if err := need(map[string]string{"id": e.ID}); err != nil {
				return nil, err
			}
			if strings.TrimSpace(e.Reason) == "" {
				return nil, bad("rule 27: a replaced step is superseded with a reason")
			}
			ops = append(ops, []string{"update", e.ID, "--add-label", "superseded"}, []string{"close", e.ID, "--reason", "Superseded: " + strings.TrimSpace(e.Reason)})
		case "close":
			if err := need(map[string]string{"id": e.ID}); err != nil {
				return nil, err
			}
			op := []string{"close", e.ID}
			if r := strings.TrimSpace(e.Reason); r != "" {
				op = append(op, "--reason", r)
			}
			ops = append(ops, op)
		default:
			return nil, bad("unknown op: use create, retitle, set_owner, set_criteria, move, link, unlink, supersede or close")
		}
	}
	return ops, nil
}
