package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example-user/commander/internal/linearmove"
	"github.com/example-user/commander/internal/linearsync"
	"github.com/example-user/commander/internal/livedata"
)

// linearSyncEvery is how often cmdrd moves linked Linear tickets to match the board.
const linearSyncEvery = 3 * time.Minute

// carrierForget is how long a ticket's rows are remembered after they leave
// the board: a workstream closed by the user's Confirm done drops off the board,
// and its ticket still has to move to Done.
const carrierForget = 30 * 24 * time.Hour

// ticketCarrierRef is one board row (workstream or track) carrying a ticket.
type ticketCarrierRef struct {
	ProjectID int64     `json:"project_id"`
	Bead      string    `json:"bead"`
	Label     string    `json:"label"`
	Seen      time.Time `json:"seen"`
}

// rowState is what the board says about one carrier.
type rowState struct {
	ref    ticketCarrierRef
	closed bool
	begun  bool
}

// begunIn: any step under it is in progress or done, or an agent works on it.
func begunIn(steps []livedata.SnapStep) bool {
	for _, s := range steps {
		if s.Superseded || s.Withdrawn {
			continue
		}
		if s.Status == "closed" || s.Status == "in_progress" || s.State == "in-progress" || s.Working {
			return true
		}
		if begunIn(s.Children) {
			return true
		}
	}
	return false
}

func stepTickets(title string, labels []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append(livedata.TitleTickets(title), labels...) {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// rowLabels names each workstream and track as the sidebar does ("#4.6 Analytics").
func rowLabels(d *livedata.Display) map[string]string {
	out := map[string]string{}
	if d == nil {
		return out
	}
	for _, w := range d.Workstreams {
		out[w.Project+":"+w.BeadID] = fmt.Sprintf("#%d %s", w.N, plain(w.Title))
		for _, t := range w.AllTracks() {
			out[w.Project+":"+t.BeadID] = fmt.Sprintf("#%d%s %s", w.N, t.Letter, plain(t.Title))
		}
	}
	return out
}

// ticketRows is every ticket the open board carries, with the rows carrying it.
// Parked, superseded and withdrawn rows carry nothing.
func ticketRows(workstreams []livedata.SnapWorkstream, labels map[string]string, now time.Time) map[string][]rowState {
	out := map[string][]rowState{}
	add := func(ws livedata.SnapWorkstream, bead, title string, tickets []string, closed, begun bool) {
		label := labels[ws.Project+":"+bead]
		if label == "" {
			label = plain(title)
		}
		for _, id := range stepTickets(title, tickets) {
			out[id] = append(out[id], rowState{ref: ticketCarrierRef{ProjectID: ws.ProjectID, Bead: bead, Label: label, Seen: now}, closed: closed, begun: begun})
		}
	}
	var walk func(ws livedata.SnapWorkstream, steps []livedata.SnapStep)
	walk = func(ws livedata.SnapWorkstream, steps []livedata.SnapStep) {
		for _, s := range steps {
			if s.Superseded || s.Withdrawn || s.Status == "deferred" {
				continue
			}
			if s.Track {
				closed := s.Status == "closed"
				add(ws, s.BeadID, s.Title, s.Tickets, closed, closed || s.Status == "in_progress" || begunIn(s.Children))
			}
			walk(ws, s.Children)
		}
	}
	for _, ws := range workstreams {
		if ws.Status == "deferred" {
			continue
		}
		closed := ws.Status == "closed"
		add(ws, ws.BeadID, ws.Title, ws.Tickets, closed, closed || begunIn(ws.Steps))
		walk(ws, ws.Steps)
	}
	return out
}

// want is where a ticket's rows put it: Done once every row carrying it is
// closed, In Progress once work on any of them began.
func want(rows []rowState) linearmove.Target {
	all, begun := len(rows) > 0, ""
	for _, r := range rows {
		all = all && r.closed
		if begun == "" && r.begun {
			begun = r.ref.Label
		}
	}
	switch {
	case all:
		return linearmove.Target{Want: linearmove.WantCompleted, Why: "you confirmed " + rows[0].ref.Label + " done"}
	case begun != "":
		return linearmove.Target{Want: linearmove.WantStarted, Why: "work began on " + begun}
	}
	return linearmove.Target{}
}

func carriersPath(home string) string { return filepath.Join(home, "linear-carriers.json") }

func readCarriers(home string) map[string][]ticketCarrierRef {
	out := map[string][]ticketCarrierRef{}
	if raw, err := os.ReadFile(carriersPath(home)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func writeCarriers(home string, c map[string][]ticketCarrierRef) {
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := carriersPath(home) + ".tmp"
	if os.WriteFile(tmp, raw, 0600) == nil {
		_ = os.Rename(tmp, carriersPath(home))
	}
}

// linearTargets is what each ticket should be, from the board now and, for a
// ticket whose rows left the board, from those rows' recorded status.
func linearTargets(rows map[string][]rowState, remembered map[string][]ticketCarrierRef, status func(projectID int64, bead string) (string, bool), now time.Time) (map[string]linearmove.Target, map[string][]ticketCarrierRef) {
	targets := map[string]linearmove.Target{}
	keep := map[string][]ticketCarrierRef{}
	for id, rs := range rows {
		targets[id] = want(rs)
		for _, r := range rs {
			keep[id] = append(keep[id], r.ref)
		}
	}
	for id, refs := range remembered {
		if _, on := rows[id]; on || len(refs) == 0 || now.Sub(refs[0].Seen) > carrierForget {
			continue
		}
		var rs []rowState
		known := true
		for _, ref := range refs {
			st, ok := status(ref.ProjectID, ref.Bead)
			if !ok {
				known = false
				break
			}
			rs = append(rs, rowState{ref: ref, closed: st == "closed"})
		}
		if !known {
			continue
		}
		if t := want(rs); t.Want == linearmove.WantCompleted {
			targets[id] = t // a move already made reads back as done and is skipped
		}
		keep[id] = refs
	}
	return targets, keep
}

// runLinearSync keeps the tickets linked to board rows in step with the work.
func (d *Daemon) runLinearSync(ctx context.Context) {
	t := time.NewTicker(linearSyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		snap := d.live.Snapshot(ctx)
		if snap.Degraded != nil || snap.OrchestrationMode != "active" {
			continue
		}
		key, err := linearsync.ReadKey(d.home)
		if err != nil || key == "" {
			continue
		}
		now := time.Now()
		rows := ticketRows(snap.Workstreams, rowLabels(snap.Display), now)
		status := func(pid int64, bead string) (string, bool) {
			var s string
			if err := d.store.DB().QueryRowContext(ctx, `SELECT status FROM bead_tasks WHERE project_id=? AND id=? AND deleted_at IS NULL`, pid, bead).Scan(&s); err != nil {
				return "", false
			}
			return s, true
		}
		targets, keep := linearTargets(rows, readCarriers(d.home), status, now)
		writeCarriers(d.home, keep)
		moves, err := linearmove.Sync(ctx, linearmove.Options{Home: d.home}, targets)
		for _, m := range moves {
			log.Printf("linear: moved %s from %s to %s (%s)", m.Ticket, m.From, m.To, m.Why)
		}
		if err != nil {
			log.Printf("linear: sync incomplete: %v", err)
		}
	}
}
