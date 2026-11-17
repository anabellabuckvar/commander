package beads

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example-user/commander/internal/store"
)

// shapeGuardEnabled says whether a write is checked: every versioned plan,
// every write by the Commander, and tests that opt in. A file switch lets an
// operator turn the guard off in an emergency without a rebuild.
func shapeGuardEnabled(ctx context.Context, ops [][]string) bool {
	if v := os.Getenv("CMDR_SHAPE_CHECK"); v == "0" {
		return false
	}
	if home := os.Getenv("COMMANDER_HOME"); home != "" {
		if _, e := os.Stat(filepath.Join(home, "shape-check-off")); e == nil {
			return false
		}
	} else if h, e := os.UserHomeDir(); e == nil {
		if _, e := os.Stat(filepath.Join(h, ".commander", "shape-check-off")); e == nil {
			return false
		}
	}
	structural := false
	for _, op := range ops {
		if len(op) > 0 && op[0] != "comments" && op[0] != "set-state" {
			structural = true
		}
	}
	if !structural {
		return false
	}
	return planBasisOf(ctx) != nil || pgCallerValue(ctx, "COMMANDER_ROLE") == "commander" || os.Getenv("CMDR_SHAPE_CHECK") == "1"
}

type shapeBatch struct {
	touched    map[string]bool
	creates    map[string]int
	referenced map[string]bool
}

func parseShapeBatch(ops [][]string) shapeBatch {
	b := shapeBatch{touched: map[string]bool{}, creates: map[string]int{}, referenced: map[string]bool{}}
	note := func(id string) {
		if id != "" && !strings.HasPrefix(id, "$") {
			b.touched[id] = true
			b.referenced[id] = true
		}
	}
	for _, op := range ops {
		if len(op) < 2 || op[0] == "comments" {
			if len(op) >= 3 && op[0] == "comments" {
				b.referenced[op[2]] = true
			}
			continue
		}
		pos, f, e := parsePGFlags(op[1:], "--title --description --acceptance --notes --status --priority --type --labels --add-label --remove-label --parent --assignee --reason --force --silent --no-inherit-labels --claim --ref --mode --dry-run --source-project --target-project")
		if e != nil {
			continue
		}
		switch op[0] {
		case "create":
			if p := f.one("--parent"); p != "" {
				b.creates[p]++
				note(p)
			}
		case "dep":
			if len(pos) == 3 {
				note(pos[1])
				note(pos[2])
			}
		case "link":
			if len(pos) == 2 {
				note(pos[0])
				note(pos[1])
			}
		default:
			if len(pos) > 0 {
				note(pos[0])
			}
			if p := f.one("--parent"); p != "" {
				note(p)
			}
		}
	}
	return b
}

// pgShapeRoots maps beads to the workstream roots whose trees they live in.
func pgShapeRoots(ctx context.Context, q store.Querier, pid int64, ids map[string]bool) ([]string, error) {
	roots := map[string]bool{}
	for id := range ids {
		cur := id
		for depth := 0; depth < 32 && cur != ""; depth++ {
			var parent string
			e := q.QueryRowContext(ctx, `SELECT COALESCE(parent_id,'') FROM bead_tasks WHERE project_id=? AND id=? AND deleted_at IS NULL`, pid, cur).Scan(&parent)
			if e != nil {
				break
			}
			// The top of the tree: a track's rules need its siblings, so a
			// nested workstream is checked with the workstream above it.
			if parent == "" {
				roots[cur] = true
				break
			}
			cur = parent
		}
	}
	out := make([]string, 0, len(roots))
	for r := range roots {
		out = append(out, r)
	}
	return out, nil
}

// pgShapeLoad reads the subtrees under roots as shape nodes.
func pgShapeLoad(ctx context.Context, q store.Querier, pid int64, roots []string) ([]ShapeNode, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(roots)), ",")
	args := []any{pid}
	for _, r := range roots {
		args = append(args, r)
	}
	args = append(args, pid)
	rows, e := q.QueryContext(ctx, `WITH RECURSIVE t AS (
  SELECT id FROM bead_tasks WHERE project_id=? AND deleted_at IS NULL AND id IN (`+marks+`)
  UNION
  SELECT b.id FROM bead_tasks b JOIN t ON b.parent_id=t.id WHERE b.project_id=? AND b.deleted_at IS NULL)
SELECT b.id,b.title,b.status,b.labels,COALESCE(b.parent_id,''),COALESCE(b.responsible_agent_id,''),COALESCE(b.assigned_agent,''),
  (COALESCE(b.description,'')<>''),
  COALESCE((SELECT json_agg(d.depends_on_id) FROM bead_dependencies d WHERE d.project_id=b.project_id AND d.issue_id=b.id AND d.type='blocks'),'[]')
FROM bead_tasks b JOIN t ON t.id=b.id WHERE b.project_id=?`, append(args, pid)...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ShapeNode
	for rows.Next() {
		var n ShapeNode
		var labels, blockers []byte
		if e = rows.Scan(&n.ID, &n.Title, &n.Status, &labels, &n.Parent, &n.Responsible, &n.Assigned, &n.HasReason, &blockers); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(labels, &n.Labels)
		_ = json.Unmarshal(blockers, &n.Blockers)
		if !n.HasReason {
			for _, l := range n.Labels {
				if strings.HasPrefix(l, "parked-from") || strings.HasPrefix(l, "backlog:") {
					n.HasReason = true
				}
			}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

var createdID = regexp.MustCompile(`^(?:Created issue: )?([A-Za-z0-9][A-Za-z0-9._-]*)`)

// pgShapeGuard runs write inside the caller's transaction and refuses it,
// rolling everything back, when it leaves a tree with a new shape problem.
func pgShapeGuard(ctx context.Context, q store.Querier, pid int64, ops [][]string, write func() ([]string, error)) error {
	if !shapeGuardEnabled(ctx, ops) {
		_, e := write()
		return e
	}
	batch := parseShapeBatch(ops)
	rootsBefore, e := pgShapeRoots(ctx, q, pid, batch.touched)
	if e != nil {
		return e
	}
	before, e := pgShapeLoad(ctx, q, pid, rootsBefore)
	if e != nil {
		return e
	}
	outs, e := write()
	if e != nil {
		return e
	}
	ids := map[string]bool{}
	for id := range batch.touched {
		ids[id] = true
	}
	for _, o := range outs {
		for _, line := range strings.Split(o, "\n") {
			if m := createdID.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				ids[m[1]] = true
			}
		}
	}
	for _, r := range rootsBefore {
		ids[r] = true
	}
	rootsAfter, e := pgShapeRoots(ctx, q, pid, ids)
	if e != nil {
		return e
	}
	after, e := pgShapeLoad(ctx, q, pid, rootsAfter)
	if e != nil {
		return e
	}
	bad := append(ShapeReshape(before, batch.creates, batch.referenced), ShapeNew(ShapeCheck(before), ShapeCheck(after))...)
	if len(bad) > 0 {
		return errors.New(FormatShapeRefusal(bad))
	}
	return nil
}

// PlanShapeReport is the read-only check behind `cmdr plan check`.
func PlanShapeReport(ctx context.Context, dir string, roots []string) ([]ShapeViolation, error) {
	if !PostgresConfigured() {
		return nil, errors.New("plan check requires PostgreSQL")
	}
	s, e := postgresStore()
	if e != nil {
		return nil, e
	}
	q := postgresQuery(ctx, s)
	pid, e := projectID(ctx, q, dir)
	if e != nil {
		return nil, e
	}
	if len(roots) == 0 {
		rows, e := q.QueryContext(ctx, `SELECT id FROM bead_tasks WHERE project_id=? AND deleted_at IS NULL AND status<>'closed' AND COALESCE(parent_id,'')='' AND jsonb_exists(labels,'kind:workstream')`, pid)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			roots = append(roots, id)
		}
		rows.Close()
	}
	nodes, e := pgShapeLoad(ctx, q, pid, roots)
	if e != nil {
		return nil, e
	}
	return ShapeCheck(nodes), nil
}
