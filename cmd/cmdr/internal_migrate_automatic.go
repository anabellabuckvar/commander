package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/store"
)

// runInternalMigrateAutomatic moves every open step's event waits onto
// Automatic steps of their own, placed before it (rule 14: only an Automatic
// step waits). It prints what it would do; --apply writes each step's move as
// one plan batch, through the same validated door as every write.
func runInternalMigrateAutomatic(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("internal-migrate-automatic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "write the moves (default: list them)")
	only := fs.String("project", "", "one project (default: every active one)")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	path, e := stateDBPath()
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	st, e := store.Open(path)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	projects, e := st.ListProjects(ctx)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	bc := newBeadsClientFn()
	found, moved, refused := 0, 0, 0
	for _, p := range projects {
		if p.Status != "active" || (*only != "" && p.Name != *only) {
			continue
		}
		list, e := bc.List(ctx, p.Path, beads.ListOptions{Status: "all"})
		if e != nil {
			fmt.Fprintf(stderr, "%s: %v\n", p.Name, e)
			continue
		}
		for _, b := range list {
			if b.Status == "closed" || beads.Superseded(b) || slices.ContainsFunc(b.Labels, func(l string) bool {
				return l == beads.AutomaticLabel || l == "kind:workstream" || l == "kind:track" || l == "kind:task" || l == "kind:gate"
			}) {
				continue
			}
			if b.Parent == "" {
				b.Parent = b.ParentID
			}
			ops := beads.AutomaticStepOps(b)
			if len(ops) == 0 {
				continue
			}
			found++
			var waits []string
			for _, l := range b.Labels {
				if w, ok := beads.ParseWaitLabel(l); ok && beads.WatchableWait(w) {
					waits = append(waits, w.Text())
				}
			}
			line := fmt.Sprintf("%s %s %q: %s", p.Name, b.ID, b.Title, strings.Join(waits, "; "))
			if !*apply {
				fmt.Fprintln(stdout, "would move "+line)
				continue
			}
			basis, e := beads.CurrentPlanBasis(ctx, p.Path, ops)
			if e == nil {
				var res beads.DoorResponse
				res, e = beads.ApplyPlan(ctx, p.Path, ops, basis, nil)
				if e == nil && res.Exit != 0 {
					e = fmt.Errorf("%s", strings.TrimSpace(res.Error))
				}
			}
			if e != nil {
				refused++
				fmt.Fprintf(stdout, "refused %s: %v\n", line, e)
				continue
			}
			moved++
			fmt.Fprintln(stdout, "moved "+line)
		}
	}
	fmt.Fprintf(stdout, "%d steps with event waits; %d moved onto Automatic steps; %d refused\n", found, moved, refused)
	if refused > 0 {
		return 1
	}
	return 0
}
