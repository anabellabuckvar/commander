package statusclassify

import "testing"

// Nov 17: track 3.10 "One Grafana" showed "30 PM". The model wrote a line with
// a clock time, and the leading-label strip cut it at the colon in "2:30".
func TestFinishDescriptionNeverCutsAtAColonInATime(t *testing.T) {
	opener := "Waiting on check for a successful… (Nov 17 19:30 UTC):"
	for in, want := range map[string]string{
		"Waiting on the Grafana fix deploy at 2:30 PM": "Waiting on the Grafana fix deploy at 2:30 PM",
		"Commander checks prod at 2:30 PM EST":         "Commander checks prod at 2:30 PM EST",
		"Waiting on IT: Teams key due Thursday":        "Teams key due Thursday",
	} {
		if got, ok := finishDescription(opener, in); !ok || got != want {
			t.Errorf("finishDescription(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestFinishDescriptionRefusesAStub(t *testing.T) {
	for _, in := range []string{"30 PM", "2:30 PM EST", "Waiting on deploy: 30", "Soon"} {
		if got, ok := finishDescription("Waiting:", in); ok {
			t.Errorf("finishDescription(%q) = %q, accepted; want refused so the row keeps its rule-based line", in, got)
		}
	}
}
