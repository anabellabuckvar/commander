package livedata

import (
	"encoding/json"
	"strings"
	"testing"
)

// The 2026-11-17 14:35 shape: #1 shows one track (.1 bounded installs), no 1.2.
func TestWorkstreamStatusesAreTheSidebar(t *testing.T) {
	now := int64(1_800_000_000)
	snap := Snapshot{
		BoardVersions: map[string]int{"commander": 25456, "manager-x": 7},
		Display: &Display{Workstreams: []BoardRow{
			{N: 1, Title: "Commander", Kind: "about", Text: "Bounded installs on macOS and Linux", Since: now - 7200,
				Tracks: []BoardTrack{{Letter: ".1", Title: "Commander installs bounded on macOS + Linux", Kind: "needs", Text: "Confirm 1.1 is done?"}}},
			{N: 3, Title: "Forge Notify", Kind: "waiting", Text: "Waiting for the morning prod release",
				Tracks: []BoardTrack{
					{Letter: ".2", Title: "Grafana alerting through Notify", Kind: "stuck", Text: "Stuck: no progress for 1 hour"},
					{Letter: ".3", Title: "Teams delivery", Kind: "working", Text: "Agent checking",
						Children: []BoardTrack{{Letter: ".3.1", Title: "Teams key", Kind: "quiet", Text: "Parked"}}},
				}},
		}},
		NeedsYou: []NeedsYou{{Title: "Confirm done", Workstream: "Commander", TrackLetter: ".1", Ask: "Confirm 1.1 is done?", Since: now - 600}},
	}
	st := WorkstreamStatuses(snap, "commander", now)
	if st.Version != 25456 {
		t.Fatalf("version = %d, want the commander board version 25456", st.Version)
	}
	block := st.Block()
	if !strings.HasPrefix(block, `<workstream-statuses version="25456">{`) || !strings.HasSuffix(block, `}</workstream-statuses>`) {
		t.Fatalf("block = %s", block)
	}
	var back Statuses
	if err := json.Unmarshal([]byte(st.JSON()), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Workstreams) != 2 || back.Workstreams[0].N != 1 || back.Workstreams[1].N != 3 {
		t.Fatalf("rows = %+v, want #1 and #3 in sidebar order", back.Workstreams)
	}
	one := back.Workstreams[0]
	if len(one.Tracks) != 1 || one.Tracks[0].Letter != ".1" || one.Tracks[0].Color != "red" || one.Age != "2h" {
		t.Fatalf("#1 = %+v", one)
	}
	for _, tr := range one.Tracks {
		if tr.Letter == ".2" {
			t.Fatal("#1 has no .2 row on screen, so the statuses must not carry one")
		}
	}
	three := back.Workstreams[1]
	if three.Color != "yellow" || three.Tracks[0].Color != "orange" || three.Tracks[1].Color != "green" || three.Tracks[1].Tracks[0].Color != "grey" {
		t.Fatalf("#3 colours = %+v", three)
	}
	if len(back.NeedsYou) != 1 || back.NeedsYou[0].Where != "#1.1" || back.NeedsYou[0].Ask != "Confirm 1.1 is done?" || back.NeedsYou[0].Age != "10m" {
		t.Fatalf("needs you = %+v", back.NeedsYou)
	}
	if got := UnchangedBlock(25456); !strings.Contains(got, "workstream statuses unchanged since v25456") {
		t.Fatal(got)
	}
	if WorkstreamStatuses(snap, "manager-x", now).Version != 7 {
		t.Fatal("a Manager's statuses carry its own board version")
	}
}

// Eric wants to see who is working on what: each track carries its agents
// (name, runtime, step, state, since) and each row counts the ones working.
func TestWorkstreamStatusesCarryTheAgents(t *testing.T) {
	now := int64(1_800_000_000)
	ws := SnapWorkstream{Project: "vibe-apps", BeadID: "zq9h.6", Steps: []SnapStep{
		{BeadID: "zq9h.6.76", Title: "Release 2b", Track: true, Children: []SnapStep{
			{BeadID: "zq9h.6.76.2", Title: "2b chain 2/6: fix or drop the 03:17 validate failures"}}},
		{BeadID: "zq9h.6.63", Title: "Chart open", Track: true},
	}}
	snap := Snapshot{
		BoardVersions: map[string]int{"commander": 1},
		Workstreams:   []SnapWorkstream{ws},
		Executions: []SnapExecution{
			{Role: "operator-exec-vm-2", State: "running", BeadID: "zq9h.6.76.2", Runtime: "claude"},
			{Role: "operator-exec-vm-9", State: "exited", BeadID: "zq9h.6.63", Runtime: "codex"},
		},
		Agents: map[string]SnapAgent{"operator-exec-vm-2": {State: "working", Since: now - 1800}},
		Display: &Display{Workstreams: []BoardRow{{N: 4, Key: "vibe-apps:zq9h.6", BeadID: "zq9h.6", Title: "QA Hub Performance", Kind: "about",
			Tracks: []BoardTrack{
				{Letter: ".9", BeadID: "zq9h.6.76", Title: "Release 2b", Kind: "working", Text: "Agent: 2b chain 2/6", Since: now - 600},
				{Letter: ".2", BeadID: "zq9h.6.63", Title: "Chart open", Kind: "waiting", Text: "Waiting for: Release 2b"},
			}}}},
	}
	st := WorkstreamStatuses(snap, "commander", now)
	row := st.Workstreams[0]
	if row.AgentsWorking != 1 {
		t.Fatalf("agents_working = %d, want 1", row.AgentsWorking)
	}
	rel := row.Tracks[0]
	if rel.Age != "10m" || len(rel.Agents) != 1 {
		t.Fatalf("release track = %+v", rel)
	}
	a := rel.Agents[0]
	if a.Name != "operator-exec-vm-2" || a.Runtime != "claude" || a.State != "working" || a.Since != "30m" || !strings.HasPrefix(a.Step, "2b chain 2/6") {
		t.Fatalf("agent = %+v", a)
	}
	if len(row.Tracks[1].Agents) != 0 {
		t.Fatalf("an exited run is listed as an agent: %+v", row.Tracks[1].Agents)
	}
	if !strings.Contains(st.JSON(), `"agents_working":1`) {
		t.Fatalf("json = %s", st.JSON())
	}
}
