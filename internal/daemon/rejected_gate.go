package daemon

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
)

// finishRejectedGate carries out a "no" on a gate: what depended only on that
// gate is superseded with the same reason, and a merge gate's open PR is closed
// (same repo, no comment) when Eric or an agent authored it.
func (d *Daemon) finishRejectedGate(ctx context.Context, g store.AnsweredGate, reason string) {
	for _, id := range d.onlyDependents(ctx, g) {
		if err := closeWithReason(ctx, g.Project, id, reason); err != nil {
			log.Printf("rejected-gate: supersede %s after %s: %v", id, g.Bead, err)
			continue
		}
		log.Printf("rejected-gate: %s superseded: it depended only on %s", id, g.Bead)
	}
	if !mergeGate.MatchString(g.Title) {
		return
	}
	labels, path, err := d.store.BeadLabelsAndPath(ctx, g.Project, g.Bead)
	if err != nil {
		log.Printf("rejected-gate: %s: %v", g.Bead, err)
		return
	}
	closeRejectedPRs(ctx, runGH, ghLogin, labels, livedata.GitHubRepo(path), g.Bead)
}

// onlyDependents are the open steps that wait on the gate and on nothing else
// still open, and those that in turn wait only on them.
func (d *Daemon) onlyDependents(ctx context.Context, g store.AnsweredGate) []string {
	out, err := d.store.OnlyDependents(ctx, g.Project, g.Bead)
	if err != nil {
		log.Printf("rejected-gate: dependents of %s: %v", g.Bead, err)
	}
	return out
}

// closeRejectedPRs closes each open PR a turned-down merge gate names, when its
// author is the account cmdrd runs gh as (Eric, whose agents push as him).
func closeRejectedPRs(ctx context.Context, gh ghRunner, login func(context.Context, ghRunner) string, labels []string, projectRepo, bead string) []string {
	me := login(ctx, gh)
	var closed []string
	for _, l := range labels {
		ref, ok := strings.CutPrefix(l, "pr:")
		if !ok {
			continue
		}
		repo := projectRepo
		if r, n, found := strings.Cut(ref, "#"); found {
			repo, ref = r, n
		}
		n, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))
		if err != nil || n <= 0 || repo == "" || me == "" {
			continue
		}
		out, err := gh(ctx, "pr", "view", strconv.Itoa(n), "--repo", repo, "--json", "state,author")
		if err != nil {
			log.Printf("rejected-gate: %s: read %s#%d: %v", bead, repo, n, err)
			continue
		}
		var pr struct {
			State  string `json:"state"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
		}
		if json.Unmarshal(out, &pr) != nil || pr.State != "OPEN" {
			continue
		}
		if !strings.EqualFold(pr.Author.Login, me) {
			log.Printf("rejected-gate: %s: %s#%d is %s's PR, left open", bead, repo, n, pr.Author.Login)
			continue
		}
		if _, err := gh(ctx, "pr", "close", strconv.Itoa(n), "--repo", repo); err != nil {
			log.Printf("rejected-gate: %s: close %s#%d: %v", bead, repo, n, err)
			continue
		}
		log.Printf("rejected-gate: %s: closed %s#%d", bead, repo, n)
		closed = append(closed, repo+"#"+strconv.Itoa(n))
	}
	return closed
}

var (
	loginOnce sync.Once
	loginName string
)

// ghLogin is the account gh runs as here.
func ghLogin(ctx context.Context, gh ghRunner) string {
	loginOnce.Do(func() {
		if out, err := gh(ctx, "api", "user", "--jq", ".login"); err == nil {
			loginName = strings.TrimSpace(string(out))
		}
	})
	return loginName
}
