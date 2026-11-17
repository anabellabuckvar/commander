package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// runWorkstreamStatuses prints "workstream statuses": the board tree exactly as
// the app's tree view draws it (every workstream nested to any depth, its
// line, and every open step by column with its card's lines). It is built by
// the same function as the tree view's data (GET /api/tree) and the block the
// app attaches to the user's messages, from the same snapshot, so they never differ.
func runWorkstreamStatuses(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("workstream-statuses", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the tree as the app reads it (GET /api/tree)")
	file := fs.String("snapshot", "", "read the snapshot from this file instead of cmdrd")
	wsN := fs.Int("workstream", 0, "print workstream N exactly as the app's Workflow tab draws it")
	stepID := fs.String("step", "", "print one drawn step's full detail (bead id)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	body, err := loadSnapshot(commanderHomeDir(), *file)
	if err != nil {
		fmt.Fprintln(stderr, "workstream-statuses:", err)
		return 1
	}
	var snap livedata.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		fmt.Fprintln(stderr, "workstream-statuses: snapshot:", err)
		return 1
	}
	if *wsN > 0 || *stepID != "" {
		return printWorkflowView(snap, *wsN, *stepID, *asJSON, stdout, stderr)
	}
	now := time.Now().Unix()
	t := livedata.BuildTree(snap, now)
	if *asJSON {
		fmt.Fprintln(stdout, t.JSON())
		return 0
	}
	fmt.Fprintln(stdout, t.Text(now))
	return 0
}

// printWorkflowView prints what the user sees on a workstream's Workflow tab,
// or one step of it, from the same board cmdrd sends the app.
func printWorkflowView(snap livedata.Snapshot, n int, stepID string, asJSON bool, stdout, stderr io.Writer) int {
	if snap.Display == nil {
		fmt.Fprintln(stderr, "workstream-statuses: the snapshot has no board yet")
		return 1
	}
	now := time.Now().Unix()
	for _, r := range snap.Display.Workstreams {
		if n > 0 && r.N != n {
			continue
		}
		if stepID == "" {
			if asJSON {
				out := map[string]any{"n": r.N, "title": r.Title, "color": livedata.StatusColor(r.Kind), "text": r.Text}
				if r.Diagram != nil {
					tracks := []map[string]any{}
					for _, t := range r.Diagram.Tracks {
						tracks = append(tracks, map[string]any{"id": t.ID, "title": t.Title, "steps": livedata.FlowOf(t, now)})
					}
					out["steps"], out["tracks"], out["findings"] = livedata.FlowOf(*r.Diagram, now), tracks, r.Diagram.Findings
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				fmt.Fprintln(stdout, string(b))
				return 0
			}
			fmt.Fprint(stdout, livedata.WorkflowText(r, now))
			return 0
		}
		if r.Diagram == nil {
			continue
		}
		s, at := livedata.FindDiagramStep(*r.Diagram, stepID)
		if s == nil {
			continue
		}
		if asJSON {
			b, _ := json.MarshalIndent(s, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return 0
		}
		fmt.Fprintf(stdout, "%s · %s\n  in: %d. %s / %s\n  lane: %s  state: %s [%s]\n", s.Title, s.ID, r.N, r.Title, at, livedata.LaneLabel(s.Actor), s.Text, livedata.StatusColor(s.Kind))
		for _, kv := range [][2]string{{"why", s.Why}, {"who", s.Who}, {"approvers", s.Approvers}, {"criteria", s.Criteria}, {"progress", s.Progress}} {
			if kv[1] != "" {
				fmt.Fprintf(stdout, "  %s: %s\n", kv[0], kv[1])
			}
		}
		if s.Since > 0 {
			fmt.Fprintf(stdout, "  since: %s\n", time.Unix(s.Since, 0).UTC().Format(time.RFC3339))
		}
		if len(s.After) > 0 {
			fmt.Fprintf(stdout, "  after: %s\n", strings.Join(s.After, ", "))
		}
		for _, c := range s.Children {
			fmt.Fprintf(stdout, "  child: %s — %s · %s\n", c.Title, c.Text, c.ID)
		}
		for _, f := range r.Diagram.Findings {
			if f.Bead == s.ID {
				fmt.Fprintf(stdout, "  ! [%s] %s — fix: %s\n", f.Check, f.Why, f.Fix)
			}
		}
		return 0
	}
	if stepID != "" {
		fmt.Fprintf(stderr, "workstream-statuses: step %s is not drawn on any workstream's Workflow tab\n", stepID)
	} else {
		fmt.Fprintf(stderr, "workstream-statuses: no workstream %d on the board\n", n)
	}
	return 1
}
