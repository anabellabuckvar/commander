package beads

import (
	"strconv"
	"strings"
)

// AutomaticStepOps are the ops that move a step's event waits onto Automatic
// steps of their own, placed before it: one per wait (same wait, same start)
// under the step's parent, the step linked after each, and the waits cleared
// from the step. nil when the step carries no event wait.
func AutomaticStepOps(step Bead) [][]string {
	var ops [][]string
	var clear []string
	for i, l := range step.Labels {
		w, ok := ParseWaitLabel(l)
		if !ok || !WatchableWait(w) {
			continue
		}
		ref := "auto" + strings.ReplaceAll(step.ID, ".", "_") + "_" + strconv.Itoa(i)
		create := []string{"create", "Waiting for " + w.Text(), "--labels", "kind:objective," + AutomaticLabel, "--ref", ref, "--silent"}
		if step.Parent != "" {
			create = append(create, "--parent", step.Parent)
		}
		ops = append(ops, create, []string{"update", "$" + ref, "--add-label", l}, []string{"dep", "add", step.ID, "$" + ref})
		clear = append(clear, l)
	}
	for _, l := range clear {
		ops = append(ops, []string{"update", step.ID, "--remove-label", l})
	}
	return ops
}
