package linearmove

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example-user/commander/internal/linearsync"
)

// fakeLinear serves issue reads and state updates for tickets held in memory.
type fakeLinear struct {
	mu      sync.Mutex
	state   map[string]string // ticket -> state id
	updates []string          // "TICKET->state id"
}

var fakeStates = []linearState{
	{ID: "s-triage", Name: "Triage", Type: "triage", Position: 0},
	{ID: "s-backlog", Name: "Backlog", Type: "backlog", Position: 1},
	{ID: "s-todo", Name: "Todo", Type: "unstarted", Position: 2},
	{ID: "s-blocked", Name: "Blocked", Type: "started", Position: 2.5}, // first started state by position, as on the real teams
	{ID: "s-review", Name: "In Review", Type: "started", Position: 4},
	{ID: "s-progress", Name: "In Progress", Type: "started", Position: 3},
	{ID: "s-done", Name: "Done", Type: "completed", Position: 5},
	{ID: "s-canceled", Name: "Canceled", Type: "canceled", Position: 6},
}

func stateByID(id string) linearState {
	for _, s := range fakeStates {
		if s.ID == id {
			return s
		}
	}
	return linearState{}
}

func (f *fakeLinear) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		_ = json.Unmarshal(raw, &req)
		f.mu.Lock()
		defer f.mu.Unlock()
		id := req.Variables["id"]
		if strings.HasPrefix(req.Query, "mutation") {
			f.state[id] = req.Variables["state"]
			f.updates = append(f.updates, id+"->"+req.Variables["state"])
			s := stateByID(f.state[id])
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"state": s}}}})
			return
		}
		sid, ok := f.state[id]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issue": nil}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issue": map[string]any{
			"identifier": id, "state": stateByID(sid), "team": map[string]any{"states": map[string]any{"nodes": fakeStates}},
		}}})
	}))
}

func moverOpts(t *testing.T, srv *httptest.Server) Options {
	t.Helper()
	home := t.TempDir()
	if err := linearsync.WriteKey(home, "secret-test-key"); err != nil {
		t.Fatal(err)
	}
	return Options{Home: home, Endpoint: srv.URL, HTTP: srv.Client().Do, Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
}

func TestSyncMovesForwardByStateType(t *testing.T) {
	f := &fakeLinear{state: map[string]string{
		"T-1": "s-todo",     // begun -> In Progress (lowest-position started state, not In Review)
		"T-2": "s-triage",   // closed -> Done
		"T-3": "s-review",   // already started: begun asks for nothing
		"T-4": "s-done",     // completed: never reopened
		"T-5": "s-canceled", // canceled: never touched
		"T-6": "s-backlog",  // nothing wanted
	}}
	srv := f.server(t)
	defer srv.Close()
	opts := moverOpts(t, srv)
	moves, err := Sync(context.Background(), opts, map[string]Target{
		"T-1": {Want: WantStarted, Why: "work began on #4.6 Analytics"},
		"T-2": {Want: WantCompleted},
		"T-3": {Want: WantStarted},
		"T-4": {Want: WantStarted},
		"T-5": {Want: WantCompleted},
		"T-6": {Want: WantNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.updates, ","); got != "T-1->s-progress,T-2->s-done" {
		t.Fatalf("updates = %s", got)
	}
	if len(moves) != 2 || moves[0].From != "Todo" || moves[0].To != "In Progress" || moves[0].Why != "work began on #4.6 Analytics" {
		t.Fatalf("moves = %+v", moves)
	}
	if l := ReadLedger(opts.Home); len(l.MovesFor("T-1")) != 1 || l.Written["T-2"].State != "Done" {
		t.Fatalf("ledger = %+v", l)
	}
}

func TestSyncIsIdempotentAndLeavesAHumansChangeAlone(t *testing.T) {
	f := &fakeLinear{state: map[string]string{"T-1": "s-todo"}}
	srv := f.server(t)
	defer srv.Close()
	opts := moverOpts(t, srv)
	want := map[string]Target{"T-1": {Want: WantStarted}}
	if _, err := Sync(context.Background(), opts, want); err != nil {
		t.Fatal(err)
	}
	if moves, _ := Sync(context.Background(), opts, want); len(moves) != 0 || len(f.updates) != 1 {
		t.Fatalf("second run rewrote: %v", f.updates)
	}
	// Someone moves it back to Todo after Commander's write: Commander leaves it.
	f.mu.Lock()
	f.state["T-1"] = "s-todo"
	f.mu.Unlock()
	if moves, _ := Sync(context.Background(), opts, want); len(moves) != 0 {
		t.Fatalf("fought the human: %+v", moves)
	}
	// Even finishing the work does not override their choice.
	if moves, _ := Sync(context.Background(), opts, map[string]Target{"T-1": {Want: WantCompleted}}); len(moves) != 0 {
		t.Fatalf("fought the human: %+v", moves)
	}
}

func TestSyncCarriesOnFromItsOwnWrite(t *testing.T) {
	f := &fakeLinear{state: map[string]string{"T-1": "s-todo"}}
	srv := f.server(t)
	defer srv.Close()
	opts := moverOpts(t, srv)
	if _, err := Sync(context.Background(), opts, map[string]Target{"T-1": {Want: WantStarted}}); err != nil {
		t.Fatal(err)
	}
	moves, err := Sync(context.Background(), opts, map[string]Target{"T-1": {Want: WantCompleted}})
	if err != nil || len(moves) != 1 || moves[0].From != "In Progress" || moves[0].To != "Done" {
		t.Fatalf("moves = %+v err %v", moves, err)
	}
}

func TestSyncNeedsAKey(t *testing.T) {
	if _, err := Sync(context.Background(), Options{Home: t.TempDir()}, map[string]Target{"T-1": {Want: WantStarted}}); err != linearsync.ErrKey {
		t.Fatalf("err = %v", err)
	}
}

// Work began means In Progress, not the team's first started state ("Blocked"),
// and a Blocked Commander wrote itself is corrected; one a person set is left.
func TestSyncStartsInProgressAndCorrectsItsOwnBlocked(t *testing.T) {
	f := &fakeLinear{state: map[string]string{"T-1": "s-todo", "T-2": "s-blocked", "T-3": "s-blocked"}}
	srv := f.server(t)
	defer srv.Close()
	opts := moverOpts(t, srv)
	if err := writeLedger(opts.Home, Ledger{Written: map[string]Written{"T-2": {StateID: "s-blocked", State: "Blocked"}}}); err != nil {
		t.Fatal(err)
	}
	targets := map[string]Target{"T-1": {Want: WantStarted}, "T-2": {Want: WantStarted}, "T-3": {Want: WantStarted}}
	if _, err := Sync(context.Background(), opts, targets); err != nil {
		t.Fatal(err)
	}
	if f.state["T-1"] != "s-progress" || f.state["T-2"] != "s-progress" || f.state["T-3"] != "s-blocked" {
		t.Fatalf("states = %v", f.state)
	}
}
