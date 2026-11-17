package store

import (
	"context"
	"database/sql"
	"errors"
)

// OpenQuestion is an agent's blocking question that has no card for the user yet.
type OpenQuestion struct {
	ID, Project, Bead, Title, Question, Recommendation string
}

// QuestionsWithoutCards lists pending blocking agent questions on steps with
// no card on the user's list: the user answers them, through a card.
func (s *Store) QuestionsWithoutCards(ctx context.Context) ([]OpenQuestion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,p.name,q.bead_id,b.title,q.question,q.recommendation
FROM bead_questions q JOIN projects p ON p.id=q.project_id
JOIN bead_tasks b ON b.project_id=q.project_id AND b.id=q.bead_id AND b.deleted_at IS NULL AND b.status<>'closed'
WHERE q.state='pending' AND q.blocking AND q.kind='execution' AND q.bead_id<>''
 AND NOT EXISTS(SELECT 1 FROM needs_you n WHERE n.project_id=q.project_id AND n.bead_id=q.bead_id)
ORDER BY q.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenQuestion
	for rows.Next() {
		var o OpenQuestion
		if err := rows.Scan(&o.ID, &o.Project, &o.Bead, &o.Title, &o.Question, &o.Recommendation); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PendingQuestionOn is the pending blocking question on a step and the
// planning owner that records its answer ("" when there is none).
func (s *Store) PendingQuestionOn(ctx context.Context, project, bead string) (id, owner string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT q.id,commander_planning_owner(q.project_id,q.bead_id) FROM bead_questions q JOIN projects p ON p.id=q.project_id
WHERE p.name=? AND q.bead_id=? AND q.state='pending' AND q.blocking ORDER BY q.created_at LIMIT 1`, project, bead).Scan(&id, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return id, owner, err
}
