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
	Gate                 bool
}

// AnsweredGates lists open gates carrying the user's applied answer.
func (s *Store) AnsweredGates(ctx context.Context) ([]AnsweredGate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.name,b.id,b.title,r.payload,jsonb_exists(b.labels,'gate') FROM bead_tasks b JOIN projects p ON p.id=b.project_id
JOIN LATERAL (SELECT payload,state FROM needs_resolutions n WHERE n.project_id=b.project_id AND n.bead_id=b.id ORDER BY n.rowid DESC LIMIT 1) r ON TRUE
WHERE b.deleted_at IS NULL AND b.status IN('open','in_progress') AND jsonb_exists(b.labels,'decision:answered')
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
		if err := rows.Scan(&g.Project, &g.Bead, &g.Title, &payload, &g.Gate); err != nil {
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

// BeadLabelsAndPath reads a bead's labels and its project's path.
func (s *Store) BeadLabelsAndPath(ctx context.Context, project, bead string) ([]string, string, error) {
	var raw []byte
	var path string
	if err := s.db.QueryRowContext(ctx, `SELECT b.labels,p.path FROM bead_tasks b JOIN projects p ON p.id=b.project_id WHERE p.name=? AND b.id=?`, project, bead).Scan(&raw, &path); err != nil {
		return nil, "", err
	}
	var labels []string
	_ = json.Unmarshal(raw, &labels)
	return labels, path, nil
}

// OnlyDependents are the open beads that wait on bead and on nothing else still
// open, then those that wait only on them, and so on: what a turned-down gate
// leaves with nothing to do.
func (s *Store) OnlyDependents(ctx context.Context, project, bead string) ([]string, error) {
	gone := map[string]bool{bead: true}
	var out []string
	frontier := []string{bead}
	for len(frontier) > 0 && len(out) < 64 {
		next := []string{}
		for _, id := range frontier {
			rows, err := s.db.QueryContext(ctx, `SELECT d.issue_id FROM bead_dependencies d JOIN projects p ON p.id=d.project_id JOIN bead_tasks w ON w.project_id=d.project_id AND w.id=d.issue_id
 WHERE p.name=? AND d.depends_on_id=? AND d.type='blocks' AND w.deleted_at IS NULL AND w.status<>'closed'`, project, id)
			if err != nil {
				return out, err
			}
			var waiters []string
			for rows.Next() {
				var w string
				if err := rows.Scan(&w); err != nil {
					rows.Close()
					return out, err
				}
				waiters = append(waiters, w)
			}
			rows.Close()
			for _, w := range waiters {
				if gone[w] {
					continue
				}
				brows, err := s.db.QueryContext(ctx, `SELECT d.depends_on_id FROM bead_dependencies d JOIN projects p ON p.id=d.project_id JOIN bead_tasks b ON b.project_id=d.project_id AND b.id=d.depends_on_id
 WHERE p.name=? AND d.issue_id=? AND d.type='blocks' AND b.deleted_at IS NULL AND b.status<>'closed'`, project, w)
				if err != nil {
					return out, err
				}
				only := true
				for brows.Next() {
					var b string
					if err := brows.Scan(&b); err != nil {
						brows.Close()
						return out, err
					}
					if !gone[b] {
						only = false
					}
				}
				brows.Close()
				if only {
					gone[w] = true
					out = append(out, w)
					next = append(next, w)
				}
			}
		}
		frontier = next
	}
	return out, nil
}
