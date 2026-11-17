package store

import (
	"context"
	"encoding/json"
)

// AnsweredGate is an open gate the user answered, whose answer was journaled
// and applied to the step but left the gate open.
type AnsweredGate struct {
	Project, Bead, Title string
	Choice, Text         string
	Approved             bool
}

// AnsweredGates lists open gates carrying the user's applied answer.
func (s *Store) AnsweredGates(ctx context.Context) ([]AnsweredGate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.name,b.id,b.title,r.payload FROM bead_tasks b JOIN projects p ON p.id=b.project_id
JOIN LATERAL (SELECT payload,state FROM needs_resolutions n WHERE n.project_id=b.project_id AND n.bead_id=b.id ORDER BY n.rowid DESC LIMIT 1) r ON TRUE
WHERE b.deleted_at IS NULL AND b.status IN('open','in_progress') AND jsonb_exists(b.labels,'gate') AND jsonb_exists(b.labels,'decision:answered')
 AND NOT jsonb_exists(b.labels,'closing-gate') AND r.state IN(2,3) AND NOT commander_pending_human_answer(b.project_id,b.id)
 AND NOT EXISTS(SELECT 1 FROM needs_you y WHERE y.project_id=b.project_id AND y.bead_id=b.id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnsweredGate
	for rows.Next() {
		var g AnsweredGate
		var payload string
		if err := rows.Scan(&g.Project, &g.Bead, &g.Title, &payload); err != nil {
			return nil, err
		}
		var r struct {
			Choice, Text string
			Approved     bool
		}
		_ = json.Unmarshal([]byte(payload), &r)
		g.Choice, g.Text, g.Approved = r.Choice, r.Text, r.Approved
		out = append(out, g)
	}
	return out, rows.Err()
}
