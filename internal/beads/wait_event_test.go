package beads

import (
	"testing"
	"time"
)

// Event waits round-trip through their labels, with and without an expect-by time.
func TestEventWaitsRoundTripThroughTheirLabels(t *testing.T) {
	since := time.Unix(1794937500, 0) // 2026-11-17 16:05 UTC, the Notify cutover switch
	by := time.Unix(1795023900, 0)
	cases := []string{
		"deploy:b91127b070",
		"deploy:august-innovations/vibe-apps@b91127b070",
		"pr:merged:#3776",
		"pr:green:august-innovations/august#20475",
		"pr:approved:#20475",
		"event:slack:C0BFR3WLGEM:Fleet reconcile FAILED",
		"event:notify:fleet_reconcile:FAILED|refused",
	}
	for _, arg := range cases {
		for _, withBy := range []bool{false, true} {
			a := arg
			if withBy {
				a += ",by=" + by.UTC().Format(time.RFC3339)
			}
			w, err := ParseWait(a)
			if err != nil {
				t.Fatalf("ParseWait(%q): %v", a, err)
			}
			w.Since = since
			label := w.Label()
			if len(label) > MaxLabelLen {
				t.Fatalf("%q: label %q longer than %d", a, label, MaxLabelLen)
			}
			got, ok := ParseWaitLabel(label)
			if !ok {
				t.Fatalf("%q: label %q does not parse back", a, label)
			}
			if got.Kind != w.Kind || got.Repo != w.Repo || got.Sha != w.Sha || got.PR != w.PR || got.Cond != w.Cond || got.Channel != w.Channel || got.Ref != w.Ref || !got.By.Equal(w.By) || !got.Since.Equal(since) {
				t.Fatalf("%q: round trip %+v -> %q -> %+v", a, w, label, got)
			}
			if !got.IsEventWait() {
				t.Fatalf("%q: not an event wait", a)
			}
		}
	}
}

func TestEventWaitArgumentsAreChecked(t *testing.T) {
	for _, bad := range []string{
		"pr:closed:#1", "pr:merged:#x", "event:slack::x", "event:email:C1:x", "event:slack:C1:(",
		"until:2026-11-18T00:00:00Z:the window opens,by=2026-11-18T00:00:00Z",
		"event:slack:C0BFR3WLGEM:" + string(make([]byte, 90)),
	} {
		if _, err := ParseWait(bad); err == nil {
			t.Fatalf("ParseWait(%q) accepted", bad)
		}
	}
}

func TestEventWaitsReadInTheSidebar(t *testing.T) {
	for arg, want := range map[string]string{
		"deploy:b91127b070":                       "prod deploy with b91127b",
		"pr:merged:#3776":                         "PR #3776 merged",
		"event:slack:C0BFR3WLGEM:Fleet reconcile": `slack event in C0BFR3WLGEM matching "Fleet reconcile"`,
	} {
		w, err := ParseWait(arg)
		if err != nil {
			t.Fatal(err)
		}
		if got := w.Text(); got != want {
			t.Fatalf("%s: Text %q, want %q", arg, got, want)
		}
	}
}

// A clock time standing in for something cmdrd can watch is flagged; a real time is not.
func TestEventTimerLabel(t *testing.T) {
	for _, l := range []string{"Commander checks prod after the deploy", "the next real moved notification", "PR 3787 is green", "the Grafana alert fires"} {
		if !EventTimerLabel(l) {
			t.Fatalf("%q not flagged", l)
		}
	}
	for _, l := range []string{"the 6 AM quiet window opens", "Eish is back Monday", "IT gives us the Teams key"} {
		if EventTimerLabel(l) {
			t.Fatalf("%q flagged", l)
		}
	}
}
