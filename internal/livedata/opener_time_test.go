package livedata

import (
	"strings"
	"testing"
)

func TestClipKeepTimeKeepsTheWholeTime(t *testing.T) {
	for _, who := range []string{
		"check for a successful… (Nov 17 19:30 UTC)",
		"check for a successful prod deploy that includes the Grafana fix (Nov 17 19:30 UTC)",
	} {
		got := clipKeepTime(who, 40)
		if !strings.HasSuffix(got, "(Nov 17 19:30 UTC)") {
			t.Errorf("clipKeepTime(%q) = %q: the time was cut", who, got)
		}
		if !strings.HasPrefix(got, "check for a") {
			t.Errorf("clipKeepTime(%q) = %q: the label was lost", who, got)
		}
		if strings.Contains(got, "……") {
			t.Errorf("clipKeepTime(%q) = %q: doubled ellipsis", who, got)
		}
	}
	if got := clipKeepTime("IT's Teams key", 40); got != "IT's Teams key" {
		t.Errorf("a short who changed: %q", got)
	}
}
