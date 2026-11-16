package store

import (
	"strings"
	"testing"
)

// Nov 16: approved QA Hub steps (6.63.14, 6.66.24) read "Approved; Manager
// dispatch pending" for over an hour with no Manager running. cmdrd marks such
// a step ready itself, removing only the dispatch receipt, once.
func TestPostgresDispatchApprovedMarksReadyOnceAndDropsTheReceipt(t *testing.T) {
	s, pid, ctx := jurisdictionFixture(t)
	const receipt = "w:dispatch-receipt"
	if _, e := s.DB().ExecContext(ctx, `UPDATE bead_tasks SET description='Bounded work',acceptance_criteria='Evidence passes',labels='["kind:objective","w:dispatch-receipt"]'::jsonb WHERE project_id=? AND id='leaf'`, pid); e != nil {
		t.Fatal(e)
	}
	isReceipt := func(l string) bool { return l == receipt }
	ok, e := s.DispatchApproved(ctx, pid, "leaf", isReceipt)
	if e != nil || !ok {
		t.Fatalf("dispatch = %v, %v; want it marked ready", ok, e)
	}
	var ready bool
	var labels string
	if e := s.DB().QueryRowContext(ctx, `SELECT execution_ready,labels::text FROM bead_tasks WHERE project_id=? AND id='leaf'`, pid).Scan(&ready, &labels); e != nil {
		t.Fatal(e)
	}
	if !ready || strings.Contains(labels, receipt) || !strings.Contains(labels, "kind:objective") {
		t.Fatalf("after dispatch ready=%v labels=%s", ready, labels)
	}
	if ok, e := s.DispatchApproved(ctx, pid, "leaf", isReceipt); e != nil || ok {
		t.Fatalf("second dispatch = %v, %v; want a no-op", ok, e)
	}
	// No brief: never dispatched.
	if _, e := s.DB().ExecContext(ctx, `UPDATE bead_tasks SET execution_ready=FALSE,description='',labels='["w:dispatch-receipt"]'::jsonb WHERE project_id=? AND id='leaf'`, pid); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.DispatchApproved(ctx, pid, "leaf", isReceipt); e != nil || ok {
		t.Fatalf("dispatch without a brief = %v, %v; want refused", ok, e)
	}
}
