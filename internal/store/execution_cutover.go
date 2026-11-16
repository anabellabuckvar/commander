package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Cutover facts are typed bead comments on the run they apply to, like
// execution redirects: no second task store.
const (
	// CutoverFactKind asks a running execution to save its work and stop. With
	// a runtime in To, the step then resumes from that run on To; with To empty
	// it stays stopped until the planning Commander re-plans it.
	CutoverFactKind = "execution-cutover"
	// CutoverToldKind records that the agent was told; CutoverForcedKind that
	// cmdrd stopped its process after the grace.
	CutoverToldKind   = "execution-cutover-told"
	CutoverForcedKind = "execution-cutover-forced"
	// StoppedByCommander starts the execution error of a step whose agent
	// Commander stopped.
	StoppedByCommander = "Stopped by Commander"
)

// RunningExecution is a bead whose active run is running now.
type RunningExecution struct {
	BeadID, Agent, Runtime string
	RunID                  int64
}

// RunningUnder lists the running executions on bead and its descendants.
func (s *Store) RunningUnder(ctx context.Context, pid int64, bead string) ([]RunningExecution, error) {
	if !s.postgres {
		return nil, nil
	}
	rows, e := s.db.QueryContext(ctx, `WITH RECURSIVE tree(id) AS (
  SELECT id FROM bead_tasks WHERE project_id=? AND id=?
  UNION SELECT d.issue_id FROM bead_dependencies d JOIN tree t ON d.depends_on_id=t.id WHERE d.project_id=? AND d.type='parent-child'
 )
 SELECT b.id,r.agent_name,r.execution_runtime,r.id FROM tree
 JOIN bead_tasks b ON b.project_id=? AND b.id=tree.id
 JOIN agent_runs r ON r.id=b.active_run_id
 WHERE r.status='running' AND b.deleted_at IS NULL ORDER BY b.id`, pid, bead, pid, pid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []RunningExecution
	for rows.Next() {
		var x RunningExecution
		if e := rows.Scan(&x.BeadID, &x.Agent, &x.Runtime, &x.RunID); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RequestExecutionCutover asks bead's running execution to stop, then resume
// on runtime to ("" = stay stopped). It returns the run asked, 0 when nothing
// runs or the run is already on to. Asking again for the same run and target
// returns the same run without a second request.
func (s *Store) RequestExecutionCutover(ctx context.Context, pid int64, bead, to, reason, by string) (int64, error) {
	if !s.postgres {
		return 0, errors.New("execution cutover requires PostgreSQL")
	}
	var rid int64
	e := s.WithTx(ctx, func(q Querier) error {
		if _, e := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, pid); e != nil {
			return e
		}
		var runtime string
		e := q.QueryRowContext(ctx, `SELECT r.id,r.execution_runtime FROM bead_tasks b JOIN agent_runs r ON r.id=b.active_run_id WHERE b.project_id=? AND b.id=? AND b.deleted_at IS NULL AND r.status='running' FOR UPDATE OF b`, pid, bead).Scan(&rid, &runtime)
		if errors.Is(e, sql.ErrNoRows) {
			rid = 0
			return nil
		}
		if e != nil {
			return e
		}
		if to != "" && runtime == to {
			rid = 0
			return nil
		}
		var asked sql.NullString
		if e := q.QueryRowContext(ctx, `SELECT fact_value->>'to' FROM bead_comments WHERE project_id=? AND bead_id=? AND run_id=? AND fact_kind=? ORDER BY created_at DESC,id DESC LIMIT 1`, pid, bead, rid, CutoverFactKind).Scan(&asked); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if asked.Valid && asked.String == to {
			return nil
		}
		value, _ := json.Marshal(map[string]string{"to": to, "from": runtime, "reason": reason, "by": by})
		text := fmt.Sprintf("Commander asked run %d (%s) to save its work and stop, then continue on %s. %s", rid, runtime, to, reason)
		if to == "" {
			text = fmt.Sprintf("Commander asked run %d (%s) to save its work and stop. %s", rid, runtime, reason)
		}
		return insertCutoverFact(ctx, q, pid, bead, rid, CutoverFactKind, text, string(value))
	})
	return rid, e
}

func insertCutoverFact(ctx context.Context, q Querier, pid int64, bead string, rid int64, kind, text, value string) error {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO bead_comments(project_id,bead_id,id,author,text,created_at,run_id,fact_kind,fact_definition,fact_value) VALUES(?,?,?,?,?,?,?,?,?,?::jsonb)`,
		pid, bead, kind+":"+hex.EncodeToString(id[:]), "cmdrd", text, time.Now().UTC().Format(time.RFC3339Nano), rid, kind, "", value)
	return err
}

// ExecutionCutover is a cutover still in progress: the run has not stopped
// yet (Running), or it stopped and the bead waits to resume on To.
type ExecutionCutover struct {
	ProjectID                  int64
	BeadID, Agent, ProjectPath string
	RunID                      int64
	To, From, Reason           string
	Running                    bool
	RequestedAt, ToldAt        int64
	ForcedAt                   int64
}

// ExecutionCutovers lists the cutovers in progress, the newest request per run.
func (s *Store) ExecutionCutovers(ctx context.Context) ([]ExecutionCutover, error) {
	if !s.postgres {
		return nil, nil
	}
	rows, e := s.db.QueryContext(ctx, `SELECT DISTINCT ON (c.project_id,c.bead_id,c.run_id)
  c.project_id,c.bead_id,r.agent_name,p.path,c.run_id,COALESCE(c.fact_value->>'to',''),COALESCE(c.fact_value->>'from',''),COALESCE(c.fact_value->>'reason',''),
  r.status='running',c.created_at,
  COALESCE((SELECT max(t.created_at) FROM bead_comments t WHERE t.project_id=c.project_id AND t.bead_id=c.bead_id AND t.run_id=c.run_id AND t.fact_kind=?),''),
  COALESCE((SELECT max(t.created_at) FROM bead_comments t WHERE t.project_id=c.project_id AND t.bead_id=c.bead_id AND t.run_id=c.run_id AND t.fact_kind=?),'')
 FROM bead_comments c
 JOIN bead_tasks b ON b.project_id=c.project_id AND b.id=c.bead_id
 JOIN agent_runs r ON r.id=c.run_id
 JOIN projects p ON p.id=c.project_id
 WHERE c.fact_kind=? AND b.deleted_at IS NULL AND b.status IN('open','in_progress')
  AND ((r.status='running' AND b.active_run_id=c.run_id)
   OR (r.status<>'running' AND COALESCE(c.fact_value->>'to','')<>'' AND b.execution_ready AND b.resume_run_id=c.run_id))
 ORDER BY c.project_id,c.bead_id,c.run_id,c.created_at DESC,c.id DESC`, CutoverToldKind, CutoverForcedKind, CutoverFactKind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ExecutionCutover
	for rows.Next() {
		var x ExecutionCutover
		var requested, told, forced string
		if e := rows.Scan(&x.ProjectID, &x.BeadID, &x.Agent, &x.ProjectPath, &x.RunID, &x.To, &x.From, &x.Reason, &x.Running, &requested, &told, &forced); e != nil {
			return nil, e
		}
		x.RequestedAt, x.ToldAt, x.ForcedAt = unixOf(requested), unixOf(told), unixOf(forced)
		out = append(out, x)
	}
	return out, rows.Err()
}

func unixOf(rfc string) int64 {
	if rfc == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, rfc)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// RecordCutoverStep notes that a cutover's agent was told (CutoverToldKind)
// or its process stopped (CutoverForcedKind).
func (s *Store) RecordCutoverStep(ctx context.Context, c ExecutionCutover, kind, text string) error {
	return insertCutoverFact(ctx, s.db, c.ProjectID, c.BeadID, c.RunID, kind, text, "{}")
}

// CutoverTarget is the runtime a stopped run's cutover moves its bead to
// ("" = none, or a stop).
func (s *Store) CutoverTarget(ctx context.Context, rid int64) (string, error) {
	var to string
	e := s.db.QueryRowContext(ctx, `SELECT COALESCE(c.fact_value->>'to','') FROM bead_comments c JOIN agent_runs r ON r.id=c.run_id AND r.project_id=c.project_id AND r.task_bead_id=c.bead_id WHERE c.run_id=? AND c.fact_kind=? ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, rid, CutoverFactKind).Scan(&to)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	return to, e
}

// settleCutoverExit applies a cutover when its run exits: a move re-readies
// the bead to resume from this run on the new runtime; a stop leaves it not
// ready with the reason, for the planning Commander. Neither is a result to
// review. handled is false when no cutover was asked for this run.
func settleCutoverExit(ctx context.Context, q Querier, pid int64, bead string, rid int64) (handled bool, err error) {
	// A pending blocking question keeps its checkpoint; once answered, the
	// resume takes the cutover route (UsageRecoverySource).
	var asking bool
	if e := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bead_questions x JOIN bead_tasks b ON b.project_id=x.project_id AND b.id=x.bead_id WHERE x.run_id=? AND x.project_id=? AND x.bead_id=? AND x.blocking AND x.state='pending' AND x.plan_revision=b.plan_revision)`, rid, pid, bead).Scan(&asking); e != nil || asking {
		return false, e
	}
	var to, reason string
	e := q.QueryRowContext(ctx, `SELECT COALESCE(fact_value->>'to',''),COALESCE(fact_value->>'reason','') FROM bead_comments WHERE project_id=? AND bead_id=? AND run_id=? AND fact_kind=? ORDER BY created_at DESC,id DESC LIMIT 1`, pid, bead, rid, CutoverFactKind).Scan(&to, &reason)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if to == "" {
		why := StoppedByCommander
		if reason != "" {
			why += ": " + reason
		}
		_, e = q.ExecContext(ctx, `UPDATE bead_tasks SET execution_ready=FALSE,resume_run_id=NULL,resume_plan_revision=0,execution_error=?,responsible_agent_id=commander_supervisor(project_id,id) WHERE project_id=? AND id=? AND active_run_id=?`, truncateSupervision(why, 1000), pid, bead, rid)
		return e == nil, e
	}
	result, e := q.ExecContext(ctx, `UPDATE bead_tasks b SET status='open',execution_ready=TRUE,resume_run_id=?,resume_plan_revision=b.plan_revision,execution_attempts=0,execution_due_at=0,execution_error='' WHERE b.project_id=? AND b.id=? AND b.active_run_id=? AND b.status IN('open','in_progress') AND b.result_run_id IS NULL AND btrim(b.description)<>'' AND btrim(b.acceptance_criteria)<>''`, rid, pid, bead, rid)
	if e != nil {
		return false, e
	}
	n, e := result.RowsAffected()
	return n > 0, e
}
