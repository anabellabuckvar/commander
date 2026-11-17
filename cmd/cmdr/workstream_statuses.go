package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// runWorkstreamStatuses prints "workstream statuses": exactly what the user's
// sidebar shows (rows, tracks, colours, NEEDS YOU) at the current board
// version. It is built by the same function as the block the app attaches to
// the user's messages, from the same snapshot, so the two never differ.
func runWorkstreamStatuses(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("workstream-statuses", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the JSON body only")
	role := fs.String("role", "commander", "whose board version to stamp (commander or a manager-* role)")
	file := fs.String("snapshot", "", "read the snapshot from this file instead of cmdrd")
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
	st := livedata.WorkstreamStatuses(snap, *role, time.Now().Unix())
	if *asJSON {
		fmt.Fprintln(stdout, st.JSON())
		return 0
	}
	fmt.Fprintln(stdout, st.Block())
	return 0
}
