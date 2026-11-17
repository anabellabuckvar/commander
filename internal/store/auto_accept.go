package store

import (
	"context"
	"strings"
)

// AcceptCandidate is an agent step whose run ended with a clean final report.
type AcceptCandidate struct {
	ProjectID int64
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
	rows, err := s.db.QueryContext(ctx, `SELECT b.project_id,p.path,b.id,b.title,split_part(ltrim(b.result_summary),chr(10),1)
FROM bead_tasks b JOIN projects p ON p.id=b.project_id
WHERE b.deleted_at IS NULL AND b.status IN('open','in_progress') AND b.result_ready_at>0 AND b.result_run_id IS NOT NULL
 AND lower(ltrim(b.result_summary)) LIKE 'final report%'
 AND ltrim(b.result_summary) !~* '^final report\W*(blocked|blocker|failed|fail\M|cannot|could not|unable|not done|incomplete)'
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
		if err := rows.Scan(&c.ProjectID, &c.Path, &c.Bead, &c.Title, &c.Line); err != nil {
			return nil, err
		}
		c.Line = strings.TrimSpace(c.Line)
		out = append(out, c)
	}
	return out, rows.Err()
}
