package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// NativeExecution is the existing canonical run and bead, not a second claim.
// No startup token is exposed to a chat model or persisted in host metadata.
type NativeExecution struct {
	RunID, ProjectID, ResumeSourceRunID                                                           int64
	BeadID, Agent, SessionID, ProjectPath, ProjectType, Worktree, Model, Driver, Runtime, Account string
	Title, Description, Acceptance, PlanningCommander                                             string
	UsageRecovery, CanRestart                                                                     bool
}

func (s *Store) NativeExecution(ctx context.Context, role string) (*NativeExecution, error) {
	if !s.postgres {
		return nil, errors.New("native execution binding requires PostgreSQL")
	}
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,b.project_id,b.id,r.agent_name,r.opencode_session_id,p.path,p.type,COALESCE(r.worktree_path,''),COALESCE(NULLIF(r.execution_model,''),r.model),r.execution_driver,r.execution_runtime,r.execution_account,COALESCE((SELECT resume_run_id FROM execution_slots WHERE run_id=r.id),0),b.title,b.description,b.acceptance_criteria,commander_planning_owner(b.project_id,b.id),p.status='active' AND commander_execution_allowed(b.project_id,b.id,TRUE) AND b.result_run_id IS NULL FROM bead_tasks b JOIN agent_runs r ON r.id=b.active_run_id AND r.project_id=b.project_id AND r.task_bead_id=b.id JOIN projects p ON p.id=b.project_id WHERE replace(r.agent_name,'.','-')=replace(?::text,'.','-') AND r.status='running' AND b.deleted_at IS NULL ORDER BY r.id LIMIT 2`, role)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out *NativeExecution
	for rows.Next() {
		if out != nil {
			return nil, errors.New("native identity has multiple live execution claims; reconcile before starting it")
		}
		out = &NativeExecution{}
		if e = rows.Scan(&out.RunID, &out.ProjectID, &out.BeadID, &out.Agent, &out.SessionID, &out.ProjectPath, &out.ProjectType, &out.Worktree, &out.Model, &out.Driver, &out.Runtime, &out.Account, &out.ResumeSourceRunID, &out.Title, &out.Description, &out.Acceptance, &out.PlanningCommander, &out.CanRestart); e != nil {
			return nil, e
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	// Close the result before reading the source on the same bounded pool.
	rows.Close()
	if out != nil && out.ResumeSourceRunID > 0 {
		if out.UsageRecovery, e = s.UsageRecoverySource(ctx, out.ResumeSourceRunID); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (s *Store) PlanningIdentityState(ctx context.Context, role string) (owns, retired bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT commander_identity_owns(?),EXISTS(SELECT 1 FROM jurisdiction_consolidations WHERE jsonb_exists(response->'retired_planning_candidates',?))`, role, role).Scan(&owns, &retired)
	return
}

// NativeExecutionRoles includes startup intents that committed before a daemon
// crash or failed host launch. No role-file or tmux-name discovery is needed.
func (s *Store) NativeExecutionRoles(ctx context.Context, now int64) ([]string, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT r.agent_name FROM bead_tasks b JOIN agent_runs r ON r.id=b.active_run_id WHERE r.status='running' AND r.execution_driver='native' AND b.deleted_at IS NULL AND b.execution_attempts<5 AND b.execution_due_at<=? ORDER BY r.id LIMIT 128`, now)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if e = rows.Scan(&role); e != nil {
			return nil, e
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}
func (s *Store) RecordNativeStartupAttempt(ctx context.Context, role string, rid int64, session, detail string, now int64) error {
	_, e := s.db.ExecContext(ctx, `UPDATE bead_tasks b SET execution_error=?,execution_attempts=CASE WHEN ?='' THEN 0 ELSE execution_attempts+1 END,execution_due_at=CASE WHEN ?='' THEN 0 ELSE ?+60 END FROM agent_runs r WHERE r.id=b.active_run_id AND r.id=? AND r.agent_name=? AND r.opencode_session_id=? AND r.status='running' AND r.execution_driver='native'`, truncateSupervision(detail, 1000), detail, detail, now, rid, role, session)
	return e
}

// An unanswered planning checkpoint cannot become usage recovery. After its
// answer, the typed physical usage exit can use an attributed fresh-session
// handoff; the replacement instruction includes the exact question history.
// A stopped run the Commander moved to another runtime (a cutover) resumes the
// same way.
func (s *Store) UsageRecoverySource(ctx context.Context, rid int64) (bool, error) {
	var yes bool
	e := s.db.QueryRowContext(ctx, `SELECT ((status='native-error' AND EXISTS(SELECT 1 FROM bead_comments c WHERE c.project_id=r.project_id AND c.bead_id=r.task_bead_id AND c.run_id=r.id AND c.id='native-exit:'||r.id::text AND c.fact_kind='native-usage-limit'))
  OR (status<>'running' AND COALESCE((SELECT c.fact_value->>'to' FROM bead_comments c WHERE c.project_id=r.project_id AND c.bead_id=r.task_bead_id AND c.run_id=r.id AND c.fact_kind='`+CutoverFactKind+`' ORDER BY c.created_at DESC,c.id DESC LIMIT 1),'')<>'')) AND NOT EXISTS(SELECT 1 FROM bead_questions q JOIN bead_tasks b ON b.project_id=q.project_id AND b.id=q.bead_id WHERE q.run_id=r.id AND q.blocking AND q.state='pending' AND q.plan_revision=b.plan_revision) FROM agent_runs r WHERE r.id=?`, rid).Scan(&yes)
	return yes, e
}

// DeadNativeRun is a native run marked running whose agent is gone.
type DeadNativeRun struct {
	RunID int64
	Role  string
	Why   string
}

// ReapDeadNativeRuns ends native runs marked running that have no agent behind
// them, so their step is free again (it restarts on a fresh agent, or can be
// closed or superseded): a run that never launched (its five startup attempts
// are spent, after which nothing retries it), and, when watched (the daemon has
// been up for grace, long enough to have re-adopted every live host), a run
// started over grace ago with no live process seen within grace. A live run is
// always seen: the presence check renews its evidence every 30 seconds.
func (s *Store) ReapDeadNativeRuns(ctx context.Context, now time.Time, grace time.Duration, watched bool) ([]DeadNativeRun, error) {
	if !s.postgres {
		return nil, nil
	}
	cut := now.Add(-grace).Unix()
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,r.agent_name,r.opencode_session_id,COALESCE(b.execution_attempts,0)>=5,COALESCE(b.execution_error,'') FROM agent_runs r
 LEFT JOIN bead_tasks b ON b.active_run_id=r.id AND b.project_id=r.project_id AND b.id=r.task_bead_id
 WHERE r.status='running' AND r.execution_driver='native' AND (COALESCE(b.execution_attempts,0)>=5
  OR (? AND r.spawn_time<=? AND NOT EXISTS(SELECT 1 FROM run_observations o WHERE o.run_id=r.id AND o.alive AND o.checked_at>=?)))
 ORDER BY r.id LIMIT 64`, watched, cut, cut)
	if e != nil {
		return nil, e
	}
	type dead struct {
		id                    int64
		role, session, detail string
		neverLaunched         bool
	}
	var found []dead
	for rows.Next() {
		var d dead
		if e = rows.Scan(&d.id, &d.role, &d.session, &d.neverLaunched, &d.detail); e != nil {
			rows.Close()
			return nil, e
		}
		found = append(found, d)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	var out []DeadNativeRun
	var first error
	for _, d := range found {
		why := fmt.Sprintf("no live process for over %s", grace)
		if d.neverLaunched {
			why = "it never launched (5 failed starts)"
			if d.detail != "" {
				why += ": " + truncateSupervision(d.detail, 120)
			}
		}
		if e := s.FinishNativeExecutionExit(ctx, d.role, d.id, d.session, "native-error", "", why); e != nil {
			if first == nil {
				first = fmt.Errorf("reap native run %d: %w", d.id, e)
			}
			continue
		}
		out = append(out, DeadNativeRun{RunID: d.id, Role: d.role, Why: why})
	}
	return out, first
}
