// Package linearmove moves Linear tickets linked to board rows forward as their
// work starts and finishes. It is the only Linear writer; linearsync stays read-only.
package linearmove

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example-user/commander/internal/linearapi"
	"github.com/example-user/commander/internal/linearsync"
)

// Want is where the board says a ticket's work is: begun or finished.
type Want int

const (
	WantNone      Want = iota
	WantStarted        // work on its row has begun
	WantCompleted      // its row is closed (the user confirmed it done)
)

// rank orders Linear workflow state types; a ticket only ever moves up it.
// Canceled is off the ladder: it is never touched.
func rank(stateType string) int {
	switch stateType {
	case "triage":
		return 0
	case "backlog":
		return 1
	case "unstarted":
		return 2
	case "started":
		return 3
	case "completed":
		return 4
	}
	return -1
}

func (w Want) stateType() string {
	switch w {
	case WantStarted:
		return "started"
	case WantCompleted:
		return "completed"
	}
	return ""
}

// Move is one status change Commander made in Linear.
type Move struct {
	Ticket string    `json:"ticket"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Why    string    `json:"why,omitempty"`
	At     time.Time `json:"at"`
}

// Written is the state Commander last set on a ticket. A ticket whose state
// differs from it was changed by someone since: Commander leaves it alone.
type Written struct {
	StateID string    `json:"state_id"`
	State   string    `json:"state"`
	At      time.Time `json:"at"`
}

// Ledger is what the sync remembers between runs.
type Ledger struct {
	Written map[string]Written `json:"written"`
	Moves   []Move             `json:"moves"`
}

const maxLedgerMoves = 200

func LedgerPath(home string) string { return filepath.Join(home, "linear-moves.json") }

// ReadLedger returns the saved ledger, or an empty one.
func ReadLedger(home string) Ledger {
	l := Ledger{Written: map[string]Written{}}
	raw, err := os.ReadFile(LedgerPath(home))
	if err == nil {
		_ = json.Unmarshal(raw, &l)
	}
	if l.Written == nil {
		l.Written = map[string]Written{}
	}
	return l
}

func writeLedger(home string, l Ledger) error {
	if len(l.Moves) > maxLedgerMoves {
		l.Moves = l.Moves[len(l.Moves)-maxLedgerMoves:]
	}
	raw, err := json.MarshalIndent(l, "", " ")
	if err != nil {
		return err
	}
	tmp := LedgerPath(home) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, LedgerPath(home))
}

// MovesFor is the recorded moves of one ticket, oldest first.
func (l Ledger) MovesFor(ticket string) []Move {
	var out []Move
	for _, m := range l.Moves {
		if strings.EqualFold(m.Ticket, ticket) {
			out = append(out, m)
		}
	}
	return out
}

// Target is one ticket the board wants moved, and why (for the record).
type Target struct {
	Want Want
	Why  string
}

// Options says where Linear is and where the key and ledger live.
type Options struct {
	Home     string
	HTTP     linearapi.HTTPDo
	Endpoint string // tests only; defaults to Linear
	Now      func() time.Time
}

// MaxMovesPerRun bounds the Linear calls one sync makes.
const MaxMovesPerRun = 25

const issueQuery = `query($id: String!) { issue(id: $id) { identifier state { id name type } team { states { nodes { id name type position } } } } }`
const moveMutation = `mutation($id: String!, $state: String!) { issueUpdate(id: $id, input: {stateId: $state}) { success issue { state { id name type } } } }`

type linearState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Position float64 `json:"position"`
}

// Sync moves each ticket forward to what the board wants: never backwards,
// never out of completed or canceled, and never over a change someone else made
// after Commander's last write. It returns the moves it made.
func Sync(ctx context.Context, opts Options, targets map[string]Target) ([]Move, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Endpoint == "" {
		opts.Endpoint = linearapi.Endpoint
	}
	key, err := linearsync.ReadKey(opts.Home)
	if err != nil || key == "" {
		return nil, linearsync.ErrKey
	}
	ids := make([]string, 0, len(targets))
	for id, t := range targets {
		if t.Want != WantNone {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > MaxMovesPerRun {
		// Rotate so every ticket gets its turn across runs.
		start := int(opts.Now().Unix()/60) % len(ids)
		ids = append(ids[start:], ids[:start]...)[:MaxMovesPerRun]
	}
	ledger := ReadLedger(opts.Home)
	var moves []Move
	var firstErr error
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		m, err := syncOne(ctx, opts, key, id, targets[id], &ledger)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if m != nil {
			moves = append(moves, *m)
		}
	}
	if len(moves) > 0 {
		if err := writeLedger(opts.Home, ledger); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return moves, firstErr
}

var errLinear = errors.New("linear: request failed")

func call(ctx context.Context, opts Options, key, query string, vars map[string]string, out any) error {
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, status := linearapi.Query(ctx, opts.HTTP, opts.Endpoint, key, payload)
	if status == 401 || status == 403 {
		return linearsync.ErrKey
	}
	if status/100 != 2 {
		return errLinear
	}
	body = []byte(strings.ReplaceAll(string(body), key, "[redacted]"))
	var env struct {
		Errors []json.RawMessage `json:"errors"`
		Data   json.RawMessage   `json:"data"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Errors) > 0 {
		return errLinear
	}
	if json.Unmarshal(env.Data, out) != nil {
		return errLinear
	}
	return nil
}

func syncOne(ctx context.Context, opts Options, key, id string, t Target, ledger *Ledger) (*Move, error) {
	var got struct {
		Issue *struct {
			Identifier string      `json:"identifier"`
			State      linearState `json:"state"`
			Team       struct {
				States struct {
					Nodes []linearState `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	if err := call(ctx, opts, key, issueQuery, map[string]string{"id": id}, &got); err != nil {
		return nil, err
	}
	is := got.Issue
	if is == nil {
		return nil, nil // not a ticket Linear knows (or not visible to this key)
	}
	cur := is.State
	w, owned := ledger.Written[id]
	if owned && w.StateID != cur.ID {
		return nil, nil // someone moved it after Commander did: theirs to own
	}
	want := t.Want.stateType()
	to := targetState(is.Team.States.Nodes, want)
	if to == nil {
		return nil, nil
	}
	// A state Commander itself wrote that is the right type but the wrong state
	// (an earlier version picked "Blocked" for started) is corrected in place.
	correcting := owned && cur.Type == want && cur.ID != to.ID
	if !correcting && (cur.Type == "canceled" || cur.Type == "completed" || rank(cur.Type) < 0 || rank(cur.Type) >= rank(want)) {
		return nil, nil
	}
	var res struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				State linearState `json:"state"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := call(ctx, opts, key, moveMutation, map[string]string{"id": id, "state": to.ID}, &res); err != nil {
		return nil, err
	}
	if !res.IssueUpdate.Success {
		return nil, errLinear
	}
	now := opts.Now().UTC()
	ledger.Written[id] = Written{StateID: to.ID, State: to.Name, At: now}
	m := Move{Ticket: id, From: cur.Name, To: to.Name, Why: t.Why, At: now}
	ledger.Moves = append(ledger.Moves, m)
	return &m, nil
}

// targetState is the team's state a move lands on: the one named "In Progress"
// (started) or "Done" (completed) when the team has it, else the first state of
// that type by position that is not a holding state. A team's first started
// state is often "Blocked" or "In Review", which is not "work began".
func targetState(states []linearState, want string) *linearState {
	name := map[string]string{"started": "in progress", "completed": "done"}[want]
	var first, firstPlain *linearState
	for i, s := range states {
		if s.Type != want {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(s.Name), name) {
			return &states[i]
		}
		if first == nil || s.Position < first.Position {
			first = &states[i]
		}
		if !holding(s.Name) && (firstPlain == nil || s.Position < firstPlain.Position) {
			firstPlain = &states[i]
		}
	}
	if firstPlain != nil {
		return firstPlain
	}
	return first
}

func holding(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"block", "review", "wait", "hold", "paus"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}
