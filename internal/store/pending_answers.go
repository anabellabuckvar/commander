package store

import (
	"context"
	"encoding/json"
)

// PendingAnswer is a user's recorded answer not yet applied to its step.
type PendingAnswer struct {
	Project string
	Bead    string
	Action  string
}

// PendingAnswers lists the user's recorded answers whose step has not taken
// them yet (commander_pending_human_answer): cmdrd applies them itself.
func (s *Store) PendingAnswers(ctx context.Context) ([]PendingAnswer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.name,r.bead_id,r.payload FROM needs_resolutions r JOIN projects p ON p.id=r.project_id
JOIN bead_tasks b ON b.project_id=r.project_id AND b.id=r.bead_id AND b.deleted_at IS NULL
WHERE r.state IN(0,1) AND commander_pending_human_answer(r.project_id,r.bead_id)
 AND r.generation=(SELECT CAST(COALESCE(MAX(version),0) AS TEXT) FROM pitches WHERE project_id=r.project_id AND bead_id=r.bead_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingAnswer
	for rows.Next() {
		var a PendingAnswer
		var payload string
		if err := rows.Scan(&a.Project, &a.Bead, &payload); err != nil {
			return nil, err
		}
		var r struct{ Action string }
		_ = json.Unmarshal([]byte(payload), &r)
		a.Action = r.Action
		if a.Action == "" {
			a.Action = "approve"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
