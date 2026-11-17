package promptassets

import (
	"strings"
	"testing"
)

// On 2026-11-17 the Commander told the user "the old 1.2 row stays until you
// confirm its Confirm done", and their sidebar had no 1.2. Rows are described
// only from workstream statuses, a missing row is never named, a mismatch with
// the beads is said as one; and no background polling loops.
func TestCommanderPromptDescribesRowsOnlyFromWorkstreamStatuses(t *testing.T) {
	body, err := templateFS.ReadFile("commander.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, needle := range []string{
		`<workstream-statuses version="N">`,
		"Describe workstreams, tracks, rows and cards only from workstream statuses",
		"Never name a row, track or card that workstream statuses do not show",
		"say so as a mismatch",
		"cmdr workstream-statuses",
		"workstream statuses unchanged since vN",
		"Never start background polling",
		"no command waits\nfor an approval",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("commander.md is missing %q", needle)
		}
	}
	if strings.Contains(text, "<user-screen>") {
		t.Error("commander.md still points at the retired <user-screen> block")
	}
}

// The Commander changes the tree only through the tree-edit endpoint, and a
// message's selection is what "this" means.
func TestCommanderPromptEditsTheTreeThroughOneEndpoint(t *testing.T) {
	body, err := templateFS.ReadFile("commander.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, needle := range []string{
		"the one tree-edit endpoint, `cmdr plan apply` with tree edits",
		"`cmdr plan apply --file plan.json` (or `-` for stdin) is the one way you change the tree",
		`{"op": "set_owner", "id": "<bead id>", "owner": "you|implementer|reviewer|person:<name or team>"}`,
		"`[rule N] <why> — fix: <fix>`",
		`<selection kind="workstream|step" id="<bead id>"`,
		"\"this is wrong\" is about the selected bead",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("commander.md is missing %q", needle)
		}
	}
	for _, retired := range []string{"internal-dep add|rm", "internal-close-objective --reason superseded", "internal-rename-objective", "--take-open-steps", "internal-track convert", "internal-gate link"} {
		if strings.Contains(text, retired) {
			t.Errorf("commander.md still writes the tree with %q", retired)
		}
	}
}
