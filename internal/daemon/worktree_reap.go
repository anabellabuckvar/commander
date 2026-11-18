package daemon

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// worktreeReapEvery is how often the watcher removes worktrees of ended runs.
const worktreeReapEvery = 10 * time.Minute

// gitRun runs git and returns its trimmed stdout.
type gitRun func(ctx context.Context, args ...string) (string, error)

func runGit(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// reapWorktree removes a finished run's worktree when it is clean: commits not
// on any remote are kept first under refs/archive/wt/<name>. A worktree with
// uncommitted changes is kept ("kept"). removed reports what was done.
func reapWorktree(ctx context.Context, git gitRun, wt string) (removed bool, why string) {
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		return false, "gone"
	}
	status, err := git(ctx, "-C", wt, "status", "--porcelain")
	if err != nil {
		return false, "unreadable: " + err.Error()
	}
	if status != "" {
		return false, "kept: uncommitted changes"
	}
	common, err := git(ctx, "-C", wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return false, "unreadable: " + err.Error()
	}
	if ahead, err := git(ctx, "-C", wt, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil && ahead != "0" {
		if _, err := git(ctx, "-C", wt, "update-ref", "refs/archive/wt/"+filepath.Base(wt), "HEAD"); err != nil {
			return false, "kept: could not archive its unpushed commits: " + err.Error()
		}
	}
	if _, err := git(ctx, "--git-dir", common, "worktree", "remove", wt); err != nil {
		return false, "kept: " + err.Error()
	}
	return true, "removed"
}

// reapRunWorktrees removes the clean worktrees of runs that ended, every
// worktreeReapEvery: agents leave one per run, and they filled the disk.
func (d *Daemon) reapRunWorktrees(ctx context.Context) {
	t := time.NewTicker(worktreeReapEvery)
	defer t.Stop()
	kept := map[string]bool{}
	for {
		paths, err := d.store.EndedRunWorktrees(ctx, 200)
		if err != nil {
			log.Printf("worktrees: %v", err)
		}
		n := 0
		for _, wt := range paths {
			if kept[wt] {
				continue
			}
			c, cancel := context.WithTimeout(ctx, time.Minute)
			removed, why := reapWorktree(c, runGit, wt)
			cancel()
			switch {
			case removed:
				n++
			case why != "gone":
				kept[wt] = true
				log.Printf("worktrees: %s %s", wt, why)
			}
		}
		if n > 0 {
			log.Printf("worktrees: removed %d clean worktrees of ended runs", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
