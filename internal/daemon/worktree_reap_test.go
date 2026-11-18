package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A finished run's clean worktree is removed, its unpushed commits kept under
// refs/archive/wt/; one with uncommitted changes is kept.
func TestReapWorktree(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	repo := filepath.Join(dir, "repo")
	git("init", "-q", "-b", "main", repo)
	git("-C", repo, "-c", "user.email=f@example.invalid", "-c", "user.name=F", "commit", "-q", "--allow-empty", "-m", "base")
	clean, dirty := filepath.Join(dir, "wt-clean"), filepath.Join(dir, "wt-dirty")
	git("-C", repo, "worktree", "add", "-q", "-b", "a", clean)
	git("-C", repo, "worktree", "add", "-q", "-b", "b", dirty)
	git("-C", clean, "-c", "user.email=f@example.invalid", "-c", "user.name=F", "commit", "-q", "--allow-empty", "-m", "agent work")
	if err := os.WriteFile(filepath.Join(dirty, "notes.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if removed, why := reapWorktree(ctx, runGit, clean); !removed {
		t.Fatalf("clean worktree kept: %s", why)
	}
	if _, err := os.Stat(clean); !os.IsNotExist(err) {
		t.Fatal("the clean worktree is still on disk")
	}
	if got := git("-C", repo, "log", "-1", "--format=%s", "refs/archive/wt/wt-clean"); got != "agent work" {
		t.Fatalf("unpushed commit not archived: %q", got)
	}
	if removed, why := reapWorktree(ctx, runGit, dirty); removed || !strings.HasPrefix(why, "kept: uncommitted") {
		t.Fatalf("dirty worktree: removed=%v %s", removed, why)
	}
}
