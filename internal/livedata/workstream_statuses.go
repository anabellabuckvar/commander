package livedata

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Statuses is "workstream statuses": exactly what the user's sidebar shows,
// built from the display rows the app renders (Snapshot.Display) and the
// NEEDS YOU list it shows (Snapshot.NeedsYou), at one board version. It rides
// along with each of the user's messages and `cmdr workstream-statuses`
// prints it, both from this one function, so they cannot differ.
type Statuses struct {
	Version     int         `json:"version"`
	Workstreams []StatusRow `json:"workstreams"`
	NeedsYou    []StatusAsk `json:"needs_you"`
}

// StatusRow is one workstream row as the sidebar shows it.
type StatusRow struct {
	N     int    `json:"n"`
	Title string `json:"title"`
	Color string `json:"color"`
	Text  string `json:"text"`
	Age   string `json:"age,omitempty"`
	// AgentsWorking counts the agents on its steps that are in a turn now.
	AgentsWorking int `json:"agents_working"`
	// Agents are those on its steps outside every track (a track lists its own).
	Agents []StatusAgent `json:"agents,omitempty"`
	Tracks []StatusTrack `json:"tracks,omitempty"`
}

// StatusTrack is one track row under it ("#3" + ".2").
type StatusTrack struct {
	Letter string        `json:"letter"`
	Title  string        `json:"title"`
	Color  string        `json:"color"`
	Text   string        `json:"text"`
	Age    string        `json:"age,omitempty"`
	Agents []StatusAgent `json:"agents,omitempty"`
	Tracks []StatusTrack `json:"tracks,omitempty"`
}

// StatusAgent is one agent running on a track's steps, from the same live run
// data the board shows (the executions and each agent's observed chat state).
type StatusAgent struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime,omitempty"` // claude or codex
	Step    string `json:"step"`              // the step it works on
	Since   string `json:"since,omitempty"`   // how long it has been in this state
	State   string `json:"state"`             // working | idle | waiting
}

// StatusAsk is one NEEDS YOU item as shown: where it sits, its title, the ask and its age.
type StatusAsk struct {
	Where string `json:"where,omitempty"`
	Title string `json:"title"`
	Ask   string `json:"ask,omitempty"`
	Age   string `json:"age,omitempty"`
}

// StatusColor is the colour the sidebar draws for a row's kind.
func StatusColor(kind string) string {
	switch kind {
	case "needs":
		return "red"
	case "stuck", "stalled":
		return "orange"
	case "waiting":
		return "yellow"
	case "working":
		return "green"
	case "done":
		return "done"
	}
	return "grey"
}

// WorkstreamStatuses builds the statuses of snap for role's board version.
func WorkstreamStatuses(snap Snapshot, role string, now int64) Statuses {
	out := Statuses{Version: snap.BoardVersions[role], Workstreams: []StatusRow{}, NeedsYou: []StatusAsk{}}
	if role == "" {
		out.Version = snap.BoardVersions["commander"]
	}
	if snap.Display == nil {
		return out
	}
	numbers := map[string]int{}
	steps := map[string]SnapWorkstream{}
	for _, ws := range snap.Workstreams {
		steps[ws.Project+":"+ws.BeadID] = ws
	}
	for _, w := range snap.Display.Workstreams {
		numbers[w.Title] = w.N
		ag := statusAgents{snap: snap, ws: steps[w.Key], now: now}
		row := StatusRow{N: w.N, Title: w.Title, Color: StatusColor(w.Kind), Text: w.Text,
			Age: statusAge(w.Since, now), Tracks: ag.tracks(w.Tracks)}
		inTrack := map[string]bool{}
		var mark func([]StatusTrack)
		mark = func(ts []StatusTrack) {
			for _, t := range ts {
				for _, a := range t.Agents {
					inTrack[a.Name] = true
				}
				mark(t.Tracks)
			}
		}
		mark(row.Tracks)
		for _, a := range ag.on(w.BeadID) {
			if a.State == "working" {
				row.AgentsWorking++
			}
			if !inTrack[a.Name] {
				row.Agents = append(row.Agents, a)
			}
		}
		out.Workstreams = append(out.Workstreams, row)
	}
	for _, a := range snap.NeedsYou {
		where := ""
		if n := numbers[a.Workstream]; n > 0 {
			where = fmt.Sprintf("#%d%s", n, a.TrackLetter)
		}
		ask := a.Ask
		if a.Decision != nil && a.Decision.Question != "" {
			ask = a.Decision.Question
		}
		out.NeedsYou = append(out.NeedsYou, StatusAsk{Where: where, Title: a.Title, Ask: ask, Age: statusAge(a.Since, now)})
	}
	return out
}

// statusAgents finds the agents running on a workstream's steps.
type statusAgents struct {
	snap Snapshot
	ws   SnapWorkstream
	now  int64
}

func (a statusAgents) tracks(ts []BoardTrack) []StatusTrack {
	var out []StatusTrack
	for _, t := range ts {
		out = append(out, StatusTrack{Letter: t.Letter, Title: t.Title, Color: StatusColor(t.Kind), Text: t.Text,
			Age: statusAge(t.Since, a.now), Agents: a.on(t.BeadID), Tracks: a.tracks(t.Children)})
	}
	return out
}

// on lists the agents on bead id and the steps under it, in execution order.
func (a statusAgents) on(id string) []StatusAgent {
	under := map[string]string{} // bead id -> step title
	var walk func([]SnapStep, bool)
	walk = func(level []SnapStep, in bool) {
		for _, s := range level {
			mine := in || s.BeadID == id
			if mine {
				under[s.BeadID] = plainTitle(s.Title)
			}
			walk(s.Children, mine)
		}
	}
	walk(a.ws.Steps, a.ws.BeadID == id)
	var out []StatusAgent
	for _, x := range a.snap.Executions {
		title, ok := under[x.BeadID]
		if !ok && x.TaskBeadID != "" {
			title, ok = under[x.TaskBeadID]
		}
		if !ok || x.State == "" || x.State == "exited" || x.State == "finished" || x.State == "died" {
			continue
		}
		st := StatusAgent{Name: x.Role, Runtime: x.Runtime, Step: title, State: x.State}
		if ag, ok := a.snap.Agents[x.Role]; ok && ag.State != "" {
			st.State, st.Since = ag.State, statusAge(ag.Since, a.now)
		}
		out = append(out, st)
	}
	return out
}

func statusAge(since, now int64) string {
	if since <= 0 || now <= since {
		return ""
	}
	d := time.Duration(now-since) * time.Second
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// JSON is the compact body.
func (s Statuses) JSON() string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSpace(b.String())
}

// Block is what rides along with a user message.
func (s Statuses) Block() string {
	return fmt.Sprintf("<workstream-statuses version=\"%d\">%s</workstream-statuses>", s.Version, s.JSON())
}

// UnchangedBlock replaces Block when the user's last message already carried this version.
func UnchangedBlock(version int) string {
	return fmt.Sprintf("<workstream-statuses version=\"%d\">workstream statuses unchanged since v%d</workstream-statuses>", version, version)
}
