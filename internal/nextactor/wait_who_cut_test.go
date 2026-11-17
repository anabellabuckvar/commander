package nextactor

import (
	"testing"

	"github.com/example-user/commander/internal/beads"
)

func TestWaitWhoMarksACutLabelAndKeepsTheTime(t *testing.T) {
	w, ok := beads.ParseWaitLabel("w:tono4n:un:tonvi0:check_20for_20a_20successful_20")
	if !ok {
		t.Fatal("label did not parse")
	}
	got := waitWho(w)
	want := "check for a successful… (" + w.Until.UTC().Format("Jan 2 15:04") + " UTC)"
	if got != want {
		t.Errorf("waitWho = %q, want %q", got, want)
	}
}
