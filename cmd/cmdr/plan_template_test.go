package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanTemplatePrintsGuardSafeOps(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runPlanTemplate([]string{"pr-babysit", "--parent", "W.6", "--subject", "notifications redesign"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var got struct{ Ops [][]string }
	if e := json.Unmarshal(out.Bytes(), &got); e != nil || len(got.Ops) < 6 {
		t.Fatalf("ops: %v %s", e, out.String())
	}
	var check bool
	for _, op := range got.Ops {
		if op[0] == "create" && strings.HasPrefix(op[1], "Review:") && strings.Contains(strings.Join(op, " "), "lane:agents,review") && strings.Contains(strings.Join(op, " "), "--acceptance") {
			check = true
		}
	}
	if !check || !strings.HasPrefix(got.Ops[1][1], "Notifications redesign PR") {
		t.Fatalf("missing the independent review step (lane:agents,review, with a brief) or title case: %v", got.Ops)
	}
	errb.Reset()
	if code := runPlanTemplate([]string{"nope", "--parent", "W", "--subject", "x"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "pr-babysit") {
		t.Fatalf("unknown template: %d %s", code, errb.String())
	}
}
