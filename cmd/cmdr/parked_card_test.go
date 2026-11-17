package main

import (
	"context"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/beads"
)

// Nov 16: the Commander raised "Close 3.6?" on a step it had parked and told
// the user a card was in his sidebar; parked steps never reach NEEDS YOU, so
// nothing showed. A card on a parked step is refused with what to do instead.
func TestSetObjective_RefusesACardOnAParkedStep(t *testing.T) {
	stubMail(t)
	parked := beads.Bead{ID: "ms-1.9", Title: "Forge Notify duplicate", Status: beads.StatusDeferred}
	mock := &beads.MockClient{ListResult: []beads.Bead{parked}, ShowResults: map[string]*beads.Bead{"ms-1.9": &parked}}
	setupObjectiveProject(t, mock)
	stdout, stderr, readOutput := withTempOutputFiles(t)
	if code := runInternalSetObjective([]string{"--project", "manifest", "--bead", "ms-1.9", "--state", "needs-user", "--decision", testDecision}, stdout, stderr); code == 0 {
		t.Fatal("a card on a parked step must be refused")
	}
	if _, errOut := readOutput(); !strings.Contains(errOut, "is parked") || !strings.Contains(errOut, "--unpark") {
		t.Fatalf("stderr = %s", errOut)
	}
	st, cleanup, err := needsYouStoreFn()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	p, _ := st.GetProjectByName(context.Background(), "manifest")
	if n, _ := st.GetNeedsYou(context.Background(), p.ID, "ms-1.9"); n != nil {
		t.Fatalf("a refused card was stored: %+v", n)
	}
}
