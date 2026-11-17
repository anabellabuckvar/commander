package beads

import (
	"fmt"
	"sort"
	"strings"
)

// Plan templates are the recurring workstream shapes as ready plan-apply
// operations. Each one passes ShapeCheck by construction, so starting from a
// template is the easy way to build a tree the guard accepts.

type planStep struct {
	ref, title, labels string
	after              []string
}

var planTemplates = map[string]func(subject string, ownerGo bool) []planStep{
	"pr-babysit": func(s string, _ bool) []planStep {
		return []planStep{
			{ref: "build", title: "Build " + s + " and open its PR (review skill once)", labels: "kind:objective"},
			{ref: "green", title: s + " PR: get CI green and answer Devin (fix our failures; a red check from main is reported with its cause and owner)", labels: "kind:objective", after: []string{"build"}},
			{ref: "check", title: "Commander checks the " + s + " PR (green, Devin-clean, matches the scope)", labels: "kind:objective,lane:commander", after: []string{"green"}},
			{ref: "merge", title: "Greenlight: merge the " + s + " PR", labels: "kind:objective,gate,approver:user", after: []string{"check"}},
			{ref: "live", title: s + " live: wait for the deploy and confirm it works on a real event", labels: "kind:objective", after: []string{"merge"}},
			{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"live"}},
		}
	},
	"app-release": func(s string, ownerGo bool) []planStep {
		steps := []planStep{
			{ref: "train", title: "Release " + s + ": assemble the release on the dev box", labels: "kind:objective"},
			{ref: "fix", title: "Release " + s + ": fix or drop every failing change (no weakened tests; dropped changes move to the next release with the reason)", labels: "kind:objective", after: []string{"train"}},
			{ref: "validate", title: "Release " + s + ": one full validate on the final tree, receipt written (dev box quiet)", labels: "kind:objective", after: []string{"fix"}},
		}
		deployAfter := "validate"
		if ownerGo {
			steps = append(steps, planStep{ref: "go", title: "Greenlight shipping release " + s, labels: "kind:objective,gate,approver:user", after: []string{"validate"}})
			deployAfter = "go"
		}
		return append(steps,
			planStep{ref: "deploy", title: "Release " + s + ": deploy to prod reusing the receipt (zero downtime, memory gate)", labels: "kind:objective,cmdr:release-deploy", after: []string{deployAfter}},
			planStep{ref: "watch", title: "Release " + s + ": 15-minute prod watch, then post DEPLOYED", labels: "kind:objective", after: []string{"deploy"}},
			planStep{ref: "mainline", title: "Release " + s + ": land the release on the app's mainline", labels: "kind:objective", after: []string{"watch"}},
			planStep{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"mainline"}},
		)
	},
	"investigation": func(s string, _ bool) []planStep {
		return []planStep{
			{ref: "investigate", title: "Investigate " + s + ": reproduce and find the cause, with evidence", labels: "kind:objective"},
			{ref: "check", title: "Commander checks the " + s + " findings (cause proven, evidence attached)", labels: "kind:objective,lane:commander", after: []string{"investigate"}},
			{ref: "decide", title: "Decide the " + s + " fix: proposed change, size and risk", labels: "kind:objective,gate,approver:user", after: []string{"check"}},
			{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"decide"}},
		}
	},
}

// PlanTemplateNames lists the templates in a stable order.
func PlanTemplateNames() []string {
	out := make([]string, 0, len(planTemplates))
	for n := range planTemplates {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// PlanTemplateOps returns plan-apply operations that create a template's
// steps under parent, chained so each waits on the one before it.
func PlanTemplateOps(name, parent, subject string, ownerGo bool) ([][]string, error) {
	build, ok := planTemplates[name]
	if !ok {
		return nil, fmt.Errorf("unknown plan template %q (have %s)", name, strings.Join(PlanTemplateNames(), ", "))
	}
	if strings.TrimSpace(parent) == "" || strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("plan template needs a parent and a subject")
	}
	var ops [][]string
	for _, s := range build(strings.TrimSpace(subject), ownerGo) {
		ops = append(ops, []string{"create", s.title, "--parent", parent, "--labels", s.labels, "--ref", s.ref, "--silent"})
	}
	for _, s := range build(strings.TrimSpace(subject), ownerGo) {
		for _, a := range s.after {
			ops = append(ops, []string{"dep", "add", "$" + s.ref, "$" + a})
		}
	}
	return ops, nil
}
