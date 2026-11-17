package main

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/example-user/commander/internal/harddeny"
	"github.com/example-user/commander/internal/operatorguard"
	"github.com/example-user/commander/internal/store"
)

// internal-hard-deny is the Commander's PreToolUse hook on Bash: it refuses
// the commands on the hard deny-list (internal/harddeny) outright, so nothing
// is ever left waiting on an approval prompt. Operators get the same check
// inside internal-operator-guard.
func runInternalHardDeny(args []string, stdin io.Reader, stdout io.Writer) int {
	fs := flag.NewFlagSet("internal-hard-deny", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	role := fs.String("role", "", "the chat's role")
	if fs.Parse(args) != nil {
		return 0
	}
	in := readHookInput(stdin)
	command := in.shellCommand()
	if command == "" {
		return 0
	}
	if v := hardDenyVerdict(*role, command, in.Cwd); v.Denied() {
		_, _ = stdout.Write(append(harddeny.HookOutput(v), '\n'))
		_ = operatorguard.LogRefusal(commanderHomeDir(), *role, command, "hard limit "+v.Rule+": "+v.Why, time.Now())
	}
	return 0
}

// hardDenyVerdict checks command for role. An Operator's own worktree (its
// cwd) is where it may delete freely; the deploy rule lifts only inside an
// approved release-deploy step.
func hardDenyVerdict(role, command, cwd string) harddeny.Verdict {
	ctx := harddeny.Context{DeployAllowed: true}
	if operatorguard.IsOperatorRole(role) {
		ctx.Worktree = cwd
	}
	if v := harddeny.Check(command, ctx); v.Denied() {
		return v
	}
	ctx.DeployAllowed = false
	v := harddeny.Check(command, ctx)
	if !v.Denied() || releaseDeployStep(role) {
		return harddeny.Verdict{}
	}
	return v
}

// releaseDeployStep reports whether role is an Operator whose current step
// (or a step above it) carries the release-deploy label. Any error is no.
func releaseDeployStep(role string) bool {
	if !operatorguard.IsOperatorRole(role) {
		return false
	}
	path, err := stateDBPath()
	if err != nil {
		return false
	}
	st, err := store.Open(path)
	if err != nil {
		return false
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := st.GetAgentRunByAgentName(ctx, role)
	if err != nil || run == nil || run.TaskBeadID == "" {
		return false
	}
	for id := run.TaskBeadID; id != ""; {
		if ok, err := st.BeadHasLabel(ctx, run.ProjectID, id, harddeny.ReleaseDeployLabel); err == nil && ok {
			return true
		}
		i := strings.LastIndexByte(id, '.')
		if i < 0 {
			break
		}
		id = id[:i]
	}
	return false
}

func runInternalHardDenyMain(args []string, stdout *os.File) int {
	return runInternalHardDeny(args, os.Stdin, stdout)
}
