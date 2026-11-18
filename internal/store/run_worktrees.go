package store

import "context"

// EndedRunWorktrees are worktree paths of runs that ended, which no running
// run uses: the watcher removes the clean ones.
func (s *Store) EndedRunWorktrees(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.worktree_path FROM agent_runs r WHERE r.status<>'running' AND COALESCE(r.worktree_path,'')<>''
 AND NOT EXISTS(SELECT 1 FROM agent_runs o WHERE o.status='running' AND o.worktree_path=r.worktree_path) LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
