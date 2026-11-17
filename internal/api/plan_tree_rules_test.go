package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/pgtest"
	"github.com/example-user/commander/internal/store"
)

// The tree-edit endpoint: tree edits compile to one atomic plan through the
// guarded write door. An edit that breaks a numbered rule refuses the whole
// batch with the rule's number and reason; a legal batch is written whole.
func TestPostgresTreeEditsRefusedByRuleNumberOrAppliedAtomically(t *testing.T) {
	st, dsn := pgtest.Open(t)
	home := t.TempDir()
	t.Setenv("COMMANDER_HOME", home)
	t.Setenv("CMDR_SHAPE_CHECK", "1")
	if e := os.WriteFile(filepath.Join(home, "state.postgres"), []byte(dsn), 0600); e != nil {
		t.Fatal(e)
	}
	dir := projectDir(t)
	ctx := context.Background()
	pid, e := st.InsertProject(ctx, store.Project{Name: "planning", Path: dir, Type: "knowledge", Status: "active"})
	if e != nil {
		t.Fatal(e)
	}
	// W holds one nested workstream, W.1, which holds one agent step.
	for _, b := range []struct{ id, parent, title, labels, desc, acc string }{
		{"W", "", "Notify", `["kind:workstream"]`, "", ""},
		{"W.1", "W", "Phase 1", `["kind:workstream"]`, "", ""},
		{"W.1.1", "W.1", "Build phase 1", `["kind:objective","lane:agents"]`, "Build phase 1.", "Final report with the PR."},
	} {
		if _, e = st.DB().ExecContext(ctx, `INSERT INTO bead_tasks(project_id,id,title,labels,parent_id,description,acceptance_criteria,assigned_agent,created_at,updated_at) VALUES(?,?,?,?::jsonb,NULLIF(?,''),?,?,'operator-x','now','now')`,
			pid, b.id, b.title, b.labels, b.parent, b.desc, b.acc); e != nil {
			t.Fatal(e)
		}
	}
	ts := httptest.NewServer(New(Options{Token: testToken, StateStore: st}).Handler())
	defer ts.Close()
	count := func() int {
		var n int
		if e := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM bead_tasks WHERE project_id=? AND deleted_at IS NULL`, pid).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	title := func(id string) string {
		var s string
		if e := st.DB().QueryRowContext(ctx, `SELECT title FROM bead_tasks WHERE project_id=? AND id=?`, pid, id).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	apply := func(edits ...beads.TreeEdit) beads.DoorResponse {
		t.Helper()
		ops, e := beads.CompileTreeEdits(edits)
		if e != nil {
			t.Fatal(e)
		}
		basis := beads.PlanBasis{JurisdictionRevision: 1, Beads: map[string]int64{}}
		for _, id := range []string{"W", "W.1", "W.1.1"} {
			var rev int64
			if e := st.DB().QueryRowContext(ctx, `SELECT plan_revision FROM bead_tasks WHERE project_id=? AND id=?`, pid, id).Scan(&rev); e != nil {
				t.Fatal(e)
			}
			basis.Beads[id] = rev
		}
		_, out := postWrite(t, ts.URL, testToken, beads.DoorRequest{Dir: dir, Atomic: true, NoWait: true, Plan: &basis, Ops: ops, Env: []string{"COMMANDER_ROLE=commander"}})
		return out
	}
	rows := count()

	// Illegal: a step straight under W, which holds a workstream (rule 3),
	// after a legal retitle in the same batch: nothing is written.
	out := apply(
		beads.TreeEdit{Op: "retitle", ID: "W.1.1", Title: "Build phase 1 and open its PR"},
		beads.TreeEdit{Op: "create", Parent: "W", Title: "Check the dashboards", Owner: "implementer"},
	)
	if out.Exit == 0 || !strings.Contains(out.Error, "[rule 3]") || !strings.Contains(out.Error, "nothing was written") {
		t.Fatalf("mixed workstream not refused with rule 3: %+v", out)
	}
	if count() != rows || title("W.1.1") != "Build phase 1" {
		t.Fatalf("refused batch wrote something: %d rows, title %q", count(), title("W.1.1"))
	}

	// Illegal: "Confirm done" as a step (rule 18).
	out = apply(beads.TreeEdit{Op: "create", Parent: "W.1", Title: "Confirm done", Owner: "you", Criteria: "You confirm the phase is done"})
	if out.Exit == 0 || !strings.Contains(out.Error, "[rule 18]") {
		t.Fatalf("Confirm done step not refused with rule 18: %+v", out)
	}

	// Legal: retitle, a review step after the build, an approval after the review, criteria.
	out = apply(
		beads.TreeEdit{Op: "retitle", ID: "W.1.1", Title: "Build phase 1 and open its PR"},
		beads.TreeEdit{Op: "create", Parent: "W.1", Title: "Review: the phase 1 PR is green and matches the scope", Owner: "reviewer", Ref: "review"},
		beads.TreeEdit{Op: "link", ID: "$review", After: "W.1.1"},
		beads.TreeEdit{Op: "create", Parent: "W.1", Title: "Merge the phase 1 PR", Owner: "you", Ref: "merge", Criteria: "The green, reviewed PR is merged"},
		beads.TreeEdit{Op: "link", ID: "$merge", After: "$review"},
		beads.TreeEdit{Op: "set_criteria", ID: "W.1", Criteria: "Phase 1 notices go through Notify"},
	)
	if out.Exit != 0 || !out.Atomic {
		t.Fatalf("legal batch refused: %+v", out)
	}
	if count() != rows+2 || title("W.1.1") != "Build phase 1 and open its PR" {
		t.Fatalf("legal batch not written whole: %d rows, title %q", count(), title("W.1.1"))
	}
	var criteria string
	if e = st.DB().QueryRowContext(ctx, `SELECT acceptance_criteria FROM bead_tasks WHERE project_id=? AND id='W.1'`, pid).Scan(&criteria); e != nil || criteria != "Phase 1 notices go through Notify" {
		t.Fatalf("criteria %q %v", criteria, e)
	}
	var deps int
	if e = st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM bead_dependencies WHERE project_id=? AND type='blocks'`, pid).Scan(&deps); e != nil || deps != 2 {
		t.Fatalf("links: %d %v", deps, e)
	}
}
