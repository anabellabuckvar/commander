package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestInternalHardDenyRefusesTheListAndPassesTheRest(t *testing.T) {
	t.Setenv("COMMANDER_HOME", t.TempDir())
	run := func(role, command, cwd string) string {
		var out bytes.Buffer
		in := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":` + quote(command) + `},"cwd":` + quote(cwd) + `}`
		if code := runInternalHardDeny([]string{"--role", role}, strings.NewReader(in), &out); code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out.String()
	}
	if out := run("commander", "git push --force origin main", "/home/exedev/code"); !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "force-push") {
		t.Fatalf("force-push was not refused: %s", out)
	}
	// The command that waited 11 hours on an approval on 2026-11-17 runs straight through.
	if out := run("commander", `cd /home/exedev/code/vibe-apps; sleep 2; cmdr execution ready --project-id 2 --bead b --revision 3 2>&1|tail -1`, "/home/exedev/code"); out != "" {
		t.Fatalf("a routine command was refused: %s", out)
	}
	// The Commander never deploys; an Operator outside a release-deploy step neither.
	if v := hardDenyVerdict("commander", "just deploy-prod --confirm-production", "/x"); v.Rule != "prod-deploy" {
		t.Fatalf("commander deploy = %+v", v)
	}
	if v := hardDenyVerdict("operator-exec-vm-1-x", "rm -rf build", "/home/exedev/w/exec-vm-1"); v.Denied() {
		t.Fatalf("an Operator may clear its own worktree: %+v", v)
	}
	if v := hardDenyVerdict("commander", "rm -rf build", "/home/exedev/code"); v.Rule != "rm-rf" {
		t.Fatalf("the Commander's cwd is shared: %+v", v)
	}
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
