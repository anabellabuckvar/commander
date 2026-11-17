package beads

import "testing"

// Nov 17: vibe-apps-596t.11.5's wait label was cut to fit br's 50-char limit,
// so its ref reads "check for a successful " and must show as cut.
func TestCutTimeWaitShowsItsCut(t *testing.T) {
	w, ok := ParseWaitLabel("w:tono4n:un:tonvi0:check_20for_20a_20successful_20")
	if !ok {
		t.Fatal("label did not parse")
	}
	if !w.Cut {
		t.Fatal("a label ending in a space must parse as cut")
	}
	if got, want := w.RefText(), "check for a successful…"; got != want {
		t.Errorf("RefText() = %q, want %q", got, want)
	}
	if got, want := w.Text(), "check for a successful…"; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
	whole := Wait{Kind: WaitUntil, Ref: "rollout window"}
	if got := whole.RefText(); got != "rollout window" {
		t.Errorf("an uncut ref changed: %q", got)
	}
}
