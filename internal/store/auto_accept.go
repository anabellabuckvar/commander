package store

import (
	"context"
	"strings"
)

// AcceptCandidate is an agent step whose run ended with a clean final report.
type AcceptCandidate struct {
	ProjectID int64
	Project   string
	Path      string
	Bead      string
	Title     string
	Line      string // the report's first line
}

// AgentResultsToAccept lists the open agent steps whose run ended with a clean
// final report: no gate, not the user's or someone outside's, no open steps
// under it, no question or recorded answer pending, not held. Such a step
// closes itself; review happens only in an explicit review step.
func (s *Store) AgentResultsToAccept(ctx context.Context) ([]AcceptCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.project_id,p.name,p.path,b.id,b.title,split_part(ltrim(b.result_summary),chr(10),1)
FROM bead_tasks b JOIN projects p ON p.id=b.project_id
WHERE b.deleted_at IS NULL AND b.status IN('open','in_progress') AND b.result_ready_at>0 AND b.result_run_id IS NOT NULL
 AND lower(ltrim(b.result_summary)) LIKE 'final report%'
 AND ltrim(b.result_summary) !~* '^final report\W*(blocked|blocker|failed|fail\M|cannot|could not|unable|not done|incomplete|stopped|aborted|partial|send back|sent back|sending back|changes requested|(need|needs) (correction|changes|fixes|work)|not ready|reject)'
 AND (NOT (jsonb_exists(b.labels,'review') OR jsonb_exists(b.labels,'lane:commander') OR b.title ~* '^\s*(review\M|independent review|commander (check|checks)\M)')
      OR split_part(ltrim(b.result_summary),chr(10),1) ~* '\m(pass|passed|approved)\M')
 AND NOT jsonb_exists(b.labels,'gate') AND NOT jsonb_exists(b.labels,'kind:workstream') AND NOT jsonb_exists(b.labels,'hold')
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(b.labels) l WHERE l='lane:you' OR l LIKE 'lane:elsewhere%' OR l LIKE 'approver:%' OR l='closing-gate')
 AND NOT EXISTS(SELECT 1 FROM bead_tasks c WHERE c.project_id=b.project_id AND c.parent_id=b.id AND c.deleted_at IS NULL AND c.status<>'closed' AND NOT jsonb_exists(c.labels,'kind:task'))
 AND NOT EXISTS(SELECT 1 FROM bead_questions q WHERE q.project_id=b.project_id AND q.bead_id=b.id AND q.blocking AND q.state='pending')
 AND NOT commander_pending_human_answer(b.project_id,b.id)
 AND NOT EXISTS(SELECT 1 FROM agent_runs r WHERE r.id=b.active_run_id AND r.status='running')
ORDER BY b.result_ready_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AcceptCandidate
	for rows.Next() {
		var c AcceptCandidate
		if err := rows.Scan(&c.ProjectID, &c.Project, &c.Path, &c.Bead, &c.Title, &c.Line); err != nil {
			return nil, err
		}
		c.Line = strings.TrimSpace(c.Line)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SentBackReview is a review step whose run ended sending the work back.
type SentBackReview struct {
	ProjectID           int64
	Project, Path       string
	Bead, Title, Parent string
	Summary             string // the review's final report: its findings
}

// ReviewsSentBack lists open review steps whose run ended with a SEND BACK
// (or blocked) final report and no revision yet: the system writes the
// revision, then the review runs again when it closes.
func (s *Store) ReviewsSentBack(ctx context.Context) ([]SentBackReview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.project_id,p.name,p.path,b.id,b.title,COALESCE(b.parent_id,''),LEFT(b.result_summary,6000)
FROM bead_tasks b JOIN projects p ON p.id=b.project_id
WHERE b.deleted_at IS NULL AND b.status IN('open','in_progress') AND b.result_ready_at>0 AND b.result_run_id IS NOT NULL
 AND (jsonb_exists(b.labels,'review') OR b.title ~* '^\s*review\M')
 AND ltrim(b.result_summary) ~* '^final report\W*(send back|sent back|sending back|changes requested|blocked|blocker|fail|reject|not ready|(need|needs) (correction|changes|fixes|work))'
 AND NOT EXISTS(SELECT 1 FROM agent_runs r WHERE r.id=b.active_run_id AND r.status='running')
 AND NOT EXISTS(SELECT 1 FROM bead_tasks v WHERE v.project_id=b.project_id AND v.deleted_at IS NULL AND v.status<>'closed' AND jsonb_exists(v.labels,'revises:'||b.id))
ORDER BY b.result_ready_at LIMIT 16`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SentBackReview
	for rows.Next() {
		var r SentBackReview
		if err := rows.Scan(&r.ProjectID, &r.Project, &r.Path, &r.Bead, &r.Title, &r.Parent, &r.Summary); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RerunAfterRevision clears a sent-back review's result and makes it ready
// again: it is blocked by its revision, so the watcher runs it once that closes.
func (s *Store) RerunAfterRevision(ctx context.Context, pid int64, bead string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bead_tasks b SET plan_revision=plan_revision+1,execution_ready=TRUE,execution_attempts=0,execution_due_at=0,execution_error='',result_run_id=NULL,result_ready_at=0,result_summary=''
 WHERE b.project_id=? AND b.id=? AND b.status IN('open','in_progress') AND NOT EXISTS(SELECT 1 FROM agent_runs r WHERE r.id=b.active_run_id AND r.status='running')`, pid, bead)
	return err
}
