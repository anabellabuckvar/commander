package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// BeadHasLabel reports whether the bead carries label.
func (s *Store) BeadHasLabel(ctx context.Context, projectID int64, beadID, label string) (bool, error) {
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT labels FROM bead_tasks WHERE project_id = ? AND id = ? AND deleted_at IS NULL`, projectID, beadID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || !raw.Valid {
		return false, err
	}
	var labels []string
	if err := json.Unmarshal([]byte(raw.String), &labels); err != nil {
		return false, err
	}
	for _, l := range labels {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}
