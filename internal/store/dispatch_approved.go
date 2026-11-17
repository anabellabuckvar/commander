package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// DispatchApproved starts approved work its Manager would have dispatched: in
// one transaction it removes the Manager dispatch receipts (handoff reports
// which labels those are) and marks the bead ready, so the execution
// dispatcher claims it through the usual door: capacity, one worker per bead,
// the runtime rules. It refuses whatever is not plainly dispatchable now
// (closed, already ready or running, no brief, or no receipt left) and reports
// false without changing anything.
func (s *Store) DispatchApproved(ctx context.Context, pid int64, bead string, handoff func(label string) bool) (bool, error) {
	return s.dispatch(ctx, pid, bead, handoff, true)
}

// DispatchReady starts an unblocked agent step with no receipt needed: the
// system starts an agent for every unblocked step that has a brief. A
// Manager dispatch receipt on it is removed the same way.
func (s *Store) DispatchReady(ctx context.Context, pid int64, bead string, handoff func(label string) bool) (bool, error) {
	return s.dispatch(ctx, pid, bead, handoff, false)
}

func (s *Store) dispatch(ctx context.Context, pid int64, bead string, handoff func(label string) bool, needReceipt bool) (bool, error) {
	if !s.postgres {
		return false, errors.New("dispatch requires PostgreSQL")
	}
	done := false
	e := s.WithTx(ctx, func(q Querier) error {
		if _, e := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, pid); e != nil {
			return e
		}
		var status, description, acceptance, raw string
		var ready, active bool
		if e := q.QueryRowContext(ctx, `SELECT b.status,b.description,b.acceptance_criteria,b.execution_ready,b.labels::text,EXISTS(SELECT 1 FROM agent_runs r WHERE r.id=b.active_run_id AND r.status='running') FROM bead_tasks b WHERE b.project_id=? AND b.id=? AND b.deleted_at IS NULL FOR UPDATE OF b`, pid, bead).Scan(&status, &description, &acceptance, &ready, &raw, &active); e != nil {
			return e
		}
		var labels []string
		if e := json.Unmarshal([]byte(raw), &labels); e != nil {
			return e
		}
		receipts := slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return !handoff(l) })
		if needReceipt && (status != "open" || ready || active || len(receipts) == 0 || strings.TrimSpace(description) == "" || strings.TrimSpace(acceptance) == "") {
			return nil
		}
		if !needReceipt {
			// The watcher: one live agent per step, never a second.
			var live bool
			if e := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs r WHERE r.project_id=? AND r.task_bead_id=? AND r.status='running')`, pid, bead).Scan(&live); e != nil {
				return e
			}
			if (status != "open" && status != "in_progress") || ready || active || live {
				return nil
			}
		}
		kept, e := json.Marshal(slices.DeleteFunc(labels, handoff))
		if e != nil {
			return e
		}
		// A step whose agent died holds cmdrd's note of that as its result: clear
		// it so the next agent can claim the step.
		if _, e := q.ExecContext(ctx, `UPDATE bead_tasks SET status='open',result_run_id=CASE WHEN result_summary ~ '^(Execution exited without|Execution ended with an error|Mac execution ended)' THEN NULL ELSE result_run_id END,result_ready_at=CASE WHEN result_summary ~ '^(Execution exited without|Execution ended with an error|Mac execution ended)' THEN 0 ELSE result_ready_at END WHERE project_id=? AND id=? AND status IN('open','in_progress')`, pid, bead); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `UPDATE bead_tasks SET plan_revision=plan_revision+1,execution_ready=TRUE,execution_attempts=0,execution_due_at=0,execution_error='',labels=?::jsonb WHERE project_id=? AND id=?`, string(kept), pid, bead); e != nil {
			return e
		}
		done = true
		return nil
	})
	return done, e
}
