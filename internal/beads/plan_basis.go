package beads

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CurrentPlanBasis reads the tree versions a batch of ops depends on right
// now: the project's jurisdiction revision and the revision of every bead the
// ops name. A plan sent without a basis (tree edits read from the board a
// moment ago) is checked against these, under the same lock as the write.
func CurrentPlanBasis(ctx context.Context, dir string, ops [][]string) (PlanBasis, error) {
	if !PostgresConfigured() {
		return PlanBasis{}, errors.New("versioned planning requires PostgreSQL")
	}
	s, e := postgresStore()
	if e != nil {
		return PlanBasis{}, e
	}
	q := postgresQuery(ctx, s)
	pid, e := projectID(ctx, q, dir)
	if e != nil {
		return PlanBasis{}, e
	}
	basis := PlanBasis{Beads: map[string]int64{}}
	if e = q.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM jurisdiction_projects WHERE project_id=?),1)`, pid).Scan(&basis.JurisdictionRevision); e != nil {
		return PlanBasis{}, e
	}
	ids := parseShapeBatch(ops).referenced
	for _, op := range ops {
		if len(op) >= 3 && op[0] == "comments" && !strings.HasPrefix(op[2], "$") {
			ids[op[2]] = true
		}
	}
	for id := range ids {
		var revision int64
		if e = q.QueryRowContext(ctx, `SELECT plan_revision FROM bead_tasks WHERE project_id=? AND id=? AND deleted_at IS NULL`, pid, id).Scan(&revision); e != nil {
			return PlanBasis{}, fmt.Errorf("bead %s: %w", id, e)
		}
		basis.Beads[id] = revision
	}
	return basis, nil
}

// ProjectDir is a registered project's path, by its name.
func ProjectDir(ctx context.Context, name string) (string, error) {
	if !PostgresConfigured() {
		return "", errors.New("project lookup requires PostgreSQL")
	}
	s, e := postgresStore()
	if e != nil {
		return "", e
	}
	var path string
	if e = postgresQuery(ctx, s).QueryRowContext(ctx, `SELECT path FROM projects WHERE name=?`, name).Scan(&path); e != nil {
		return "", fmt.Errorf("project %q: %w", name, e)
	}
	return path, nil
}
