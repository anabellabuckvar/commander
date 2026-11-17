package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/beads"
)

// A full sha does not fit a label: the refusal names the form that does.
// Labels hold 100 characters since Nov 17, so a whole sha fits and is recorded
// (cmdrd resolves a short one to the same commit either way).
func TestWaitingOnAFullShaIsRecorded(t *testing.T) {
	wt, err := beads.ParseWait("main-ci:349a7f4e1f0c2b6d8a9e3c5b7d1f2a4c6e8b0d13")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyWaiting(context.Background(), &beads.MockClient{}, "/p", "p-ws.1", []beads.Wait{wt}, time.Unix(1793904844, 0)); err != nil {
		t.Fatalf("a whole sha under the label cap was refused: %v", err)
	}
	wt.Since = time.Unix(1793904844, 0)
	if l := wt.Label(); len(l) > beads.MaxLabelLen || !strings.Contains(l, "349a7f4e1f0c2b6d8a9e3c5b7d1f2a4c6e8b0d13") {
		t.Fatalf("label %q (%d) does not hold the whole sha under %d", l, len(l), beads.MaxLabelLen)
	}
}
