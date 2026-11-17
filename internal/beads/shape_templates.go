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
			{ref: "build", title: "Build " + s + " and open its PR (review skill once)", labels: "kind:objective,lane:agents"},
			{ref: "green", title: s + " PR: get CI green and answer Devin (fix our failures; a red check from main is reported with its cause and owner)", labels: "kind:objective,lane:agents", after: []string{"build"}},
			{ref: "check", title: "Review: the " + s + " PR (green, Devin-clean, matches the scope)", labels: "kind:objective,lane:agents,review", after: []string{"green"}},
			{ref: "merge", title: "Greenlight: merge the " + s + " PR", labels: "kind:objective,gate,approver:user", after: []string{"check"}},
			{ref: "live", title: s + " live: wait on the deploy (--waiting deploy:<merge sha>, not a clock) and confirm it works on a real event", labels: "kind:objective,lane:agents", after: []string{"merge"}},
			{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"live"}},
		}
	},
	"app-release": func(s string, ownerGo bool) []planStep {
		steps := []planStep{
			{ref: "train", title: "Release " + s + ": assemble the release on the dev box", labels: "kind:objective,lane:agents"},
			{ref: "fix", title: "Release " + s + ": fix or drop every failing change (no weakened tests; dropped changes move to the next release with the reason)", labels: "kind:objective,lane:agents", after: []string{"train"}},
			{ref: "validate", title: "Release " + s + ": one full validate on the final tree, receipt written (dev box quiet)", labels: "kind:objective,lane:agents", after: []string{"fix"}},
		}
		deployAfter := "validate"
		if ownerGo {
			steps = append(steps,
				planStep{ref: "check", title: "Review: release " + s + " (validate green on the final tree, receipt written, what was dropped and why)", labels: "kind:objective,lane:agents,review", after: []string{"validate"}},
				planStep{ref: "go", title: "Greenlight shipping release " + s, labels: "kind:objective,gate,approver:user", after: []string{"check"}})
			deployAfter = "go"
		}
		return append(steps,
			planStep{ref: "deploy", title: "Release " + s + ": deploy to prod reusing the receipt (zero downtime, memory gate)", labels: "kind:objective,lane:agents,cmdr:release-deploy", after: []string{deployAfter}},
			planStep{ref: "watch", title: "Release " + s + ": 15-minute prod watch, then post DEPLOYED", labels: "kind:objective,lane:agents", after: []string{"deploy"}},
			planStep{ref: "mainline", title: "Release " + s + ": land the release on the app's mainline", labels: "kind:objective,lane:agents", after: []string{"watch"}},
			planStep{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"mainline"}},
		)
	},
	"propose-then-approve": func(s string, _ bool) []planStep {
		return []planStep{
			{ref: "propose", title: "Propose " + s + " (what, why, and how it will be checked)", labels: "kind:objective,lane:agents"},
			{ref: "check", title: "Review: the " + s + " proposal (complete, matches the scope)", labels: "kind:objective,lane:agents,review", after: []string{"propose"}},
			{ref: "approve", title: "Approve " + s, labels: "kind:objective,gate,approver:user", after: []string{"check"}},
			{ref: "build", title: "Build " + s + " and open its PR (review skill once)", labels: "kind:objective,lane:agents", after: []string{"approve"}},
			{ref: "green", title: s + " PR: get CI green and answer Devin", labels: "kind:objective,lane:agents", after: []string{"build"}},
			{ref: "check2", title: "Review: the " + s + " PR (green, Devin-clean, matches the approved proposal)", labels: "kind:objective,lane:agents,review", after: []string{"green"}},
			{ref: "merge", title: "Greenlight: merge the " + s + " PR", labels: "kind:objective,gate,approver:user", after: []string{"check2"}},
			{ref: "live", title: s + " live: wait on the deploy (--waiting deploy:<merge sha>, not a clock) and confirm it works", labels: "kind:objective,lane:agents", after: []string{"merge"}},
			{ref: "done", title: "Confirm done", labels: "kind:objective,gate,approver:user,closing-gate", after: []string{"live"}},
		}
	},
	"investigation": func(s string, _ bool) []planStep {
		return []planStep{
			{ref: "investigate", title: "Investigate " + s + ": reproduce and find the cause, with evidence", labels: "kind:objective,lane:agents"},
			{ref: "check", title: "Review: the " + s + " findings (cause proven, evidence attached)", labels: "kind:objective,lane:agents,review", after: []string{"investigate"}},
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
		title := s.title
		if r := []rune(title); len(r) > 0 {
			title = strings.ToUpper(string(r[0])) + string(r[1:])
		}
		op := []string{"create", title, "--parent", parent, "--labels", s.labels, "--ref", s.ref, "--silent"}
		if desc, acc := stepBrief(s, title); desc != "" {
			op = append(op, "--description", desc, "--acceptance", acc)
		}
		ops = append(ops, op)
	}
	for _, s := range build(strings.TrimSpace(subject), ownerGo) {
		for _, a := range s.after {
			ops = append(ops, []string{"dep", "add", "$" + s.ref, "$" + a})
		}
	}
	return ops, nil
}

// stepBrief is an agent step's brief, from its own title: a review step checks
// the result of the steps it waits on, independently of the agent that built it.
func stepBrief(s planStep, title string) (description, acceptance string) {
	if !strings.Contains(s.labels, "lane:agents") {
		return "", ""
	}
	if strings.Contains(s.labels, "review") {
		return "Review brief: " + title + ". You are an independent reviewer, not the agent that did the work. Read the result of the step(s) this waits on (their final reports, PR, run output or live evidence) and check each point named in the title against it.",
			"Final report states PASS with the evidence for each point, or BLOCKED naming each gap and where it is."
	}
	return title + ".", "Final report says what was done, with the evidence (PR, run, output) that it is done."
}
