package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// The tree view's data (GET /api/tree) and the block on each of Eric's
// messages to the Commander (GET /api/workstream-statuses) are one tree.
func TestTreeAndStatusesBlockAreOneTree(t *testing.T) {
	s, src, ts := newTestServer(t, nil)
	src.mu.Lock()
	src.snap = livedata.Snapshot{
		Workstreams: []livedata.SnapWorkstream{{N: 1, Project: "app", BeadID: "w", Title: "Notify", Status: "open", Steps: []livedata.SnapStep{
			{BeadID: "w.1", Title: "Build the cutover", Status: "open"},
			{BeadID: "w.2", Title: "Merge the cutover PR", Status: "open", Gate: true, BlockedBy: []string{"w.1"}},
		}}},
		Display: &livedata.Display{Workstreams: []livedata.BoardRow{{Key: "app:w", N: 1, Diagram: &livedata.Diagram{ID: "w", Steps: []livedata.DiagramStep{
			{ID: "w.1", Title: "Build the cutover", Actor: "operators", Kind: "working", Text: "Agent working", Runtime: "codex"},
			{ID: "w.2", Title: "Merge the cutover PR", Actor: "you", Kind: "idle", Text: "Not reached", After: []string{"w.1"}},
		}}}}},
	}
	src.mu.Unlock()
	s.Refresh(t.Context())
	auth := map[string]string{"Authorization": "Bearer " + testToken}

	var tree livedata.Tree
	resp := get(t, ts.URL+"/api/tree", auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tree: %s", resp.Status)
	}
	if e := json.NewDecoder(resp.Body).Decode(&tree); e != nil {
		t.Fatal(e)
	}
	if len(tree.Rows) != 1 || len(tree.Steps) != 2 || tree.Steps[0].Own != "Implementer agent · Codex" || tree.Steps[1].Col != 1 {
		t.Fatalf("tree: %+v", tree)
	}

	var st struct {
		Version int    `json:"version"`
		Block   string `json:"block"`
	}
	resp = get(t, ts.URL+"/api/workstream-statuses?role=commander", auth)
	if e := json.NewDecoder(resp.Body).Decode(&st); e != nil {
		t.Fatal(e)
	}
	if want := livedata.TreeBlock(tree, time.Now().Unix()); st.Block != want {
		t.Fatalf("block is not the tree:\n%s\nwant\n%s", st.Block, want)
	}
	for _, line := range []string{"1 Notify · w", "Now: [Implementer agent · Codex] Build the cutover · w.1", "Next: [You] Merge the cutover PR · w.2"} {
		if !strings.Contains(st.Block, line) {
			t.Errorf("block lacks %q:\n%s", line, st.Block)
		}
	}
	resp = get(t, fmt.Sprintf("%s/api/workstream-statuses?last=%d", ts.URL, st.Version), auth)
	if e := json.NewDecoder(resp.Body).Decode(&st); e != nil {
		t.Fatal(e)
	}
	if st.Block != livedata.UnchangedBlock(st.Version) {
		t.Fatalf("unchanged board: %s", st.Block)
	}
}
