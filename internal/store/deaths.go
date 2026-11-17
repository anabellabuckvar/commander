package store

import "context"

// DeathsInARow counts the step's most recent agents that ended without a final
// report, since the user last answered a card on it.
func (s *Store) DeathsInARow(ctx context.Context, pid int64, bead string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM bead_comments c WHERE c.run_id=r.id AND lower(ltrim(c.text)) LIKE 'final report%')
FROM agent_runs r WHERE r.project_id=? AND r.task_bead_id=? AND r.status<>'running'
 AND r.spawn_time > COALESCE((SELECT MAX(answered_at) FROM pitches p WHERE p.project_id=r.project_id AND p.bead_id=r.task_bead_id),0)
ORDER BY r.id DESC LIMIT 5`, pid, bead)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var reported bool
		if err := rows.Scan(&reported); err != nil {
			return 0, err
		}
		if reported {
			break
		}
		n++
	}
	return n, rows.Err()
}
