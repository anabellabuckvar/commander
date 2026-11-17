package livedata

import "strconv"

// MarkLiveRuntimes sets each step's LiveRuntime (claude or codex) from the
// agents running now, so the board shows the mark only while one runs.
func MarkLiveRuntimes(snap *Snapshot) {
	live := map[string]string{}
	for _, x := range snap.Executions {
		rt := x.Runtime
		if rt == "" {
			rt = "claude"
		}
		live[projectKey(x.ProjectID, x.BeadID)] = rt
		if x.TaskBeadID != "" {
			live[projectKey(x.ProjectID, x.TaskBeadID)] = rt
		}
	}
	var mark func([]SnapStep, int64)
	mark = func(steps []SnapStep, pid int64) {
		for i := range steps {
			steps[i].LiveRuntime = live[projectKey(pid, steps[i].BeadID)]
			mark(steps[i].Children, pid)
		}
	}
	for i := range snap.Workstreams {
		mark(snap.Workstreams[i].Steps, snap.Workstreams[i].ProjectID)
	}
}

func projectKey(pid int64, bead string) string {
	return strconv.FormatInt(pid, 10) + "\x00" + bead
}

// liveRuntime is the runtime of the agent running step id now, or "".
func (b board) liveRuntime(pid int64, id string) string {
	for _, x := range b.s.Executions {
		if x.ProjectID == pid && (x.BeadID == id || x.TaskBeadID == id) {
			if x.Runtime == "" {
				return "claude"
			}
			return x.Runtime
		}
	}
	return ""
}
