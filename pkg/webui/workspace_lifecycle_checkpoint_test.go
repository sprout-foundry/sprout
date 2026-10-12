//go:build !js

package webui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: test helper running git on a temp repo
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func initCheckpointRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	writeFile(t, filepath.Join(dir, "tracked.txt"), "v1\n")
	writeFile(t, filepath.Join(dir, "staged.txt"), "s1\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// repoState captures everything a checkpoint must leave alone.
type repoState struct {
	head, status, stagedFiles, indexEntries string
}

func captureRepoState(t *testing.T, dir string) repoState {
	t.Helper()
	return repoState{
		head:         gitOut(t, dir, "rev-parse", "HEAD"),
		status:       gitOut(t, dir, "status", "--porcelain=v1", "--untracked-files=all"),
		stagedFiles:  gitOut(t, dir, "diff", "--cached", "--name-status"),
		indexEntries: gitOut(t, dir, "ls-files", "-s"),
	}
}

func TestCheckpointWorkingTree_SnapshotsWithoutTouchingRepo(t *testing.T) {
	dir := initCheckpointRepo(t)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "v2 unstaged\n")
	writeFile(t, filepath.Join(dir, "staged.txt"), "s2 staged\n")
	runGit(t, dir, "add", "staged.txt")
	writeFile(t, filepath.Join(dir, "new.txt"), "untracked\n")
	writeFile(t, filepath.Join(dir, "ignored.txt"), "ignored\n")
	before := captureRepoState(t, dir)

	cp, err := checkpointWorkingTree(context.Background(), dir, 1700000000, true, "idle")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if cp == nil {
		t.Fatal("dirty tree produced no checkpoint")
	}
	if cp.WipRef != "refs/sprout/wip/1700000000" {
		t.Fatalf("ref = %q", cp.WipRef)
	}
	if after := captureRepoState(t, dir); after != before {
		t.Fatalf("checkpoint changed the repository\nbefore: %+v\nafter:  %+v", before, after)
	}
	if got := gitOut(t, dir, "rev-parse", cp.WipRef); got != cp.WipSHA {
		t.Fatalf("%s = %s, want %s", cp.WipRef, got, cp.WipSHA)
	}
	if got := gitOut(t, dir, "rev-parse", wipRefLatest); got != cp.WipSHA {
		t.Fatalf("latest = %s, want %s", got, cp.WipSHA)
	}
	if parent := gitOut(t, dir, "rev-parse", cp.WipSHA+"^"); parent != before.head {
		t.Fatalf("checkpoint parent = %s, want HEAD %s", parent, before.head)
	}
	for path, want := range map[string]string{"tracked.txt": "v2 unstaged", "staged.txt": "s2 staged", "new.txt": "untracked"} {
		if got := gitOut(t, dir, "show", cp.WipSHA+":"+path); got != want {
			t.Errorf("%s in checkpoint = %q, want %q", path, got, want)
		}
	}
	if files := gitOut(t, dir, "ls-tree", "-r", "--name-only", cp.WipSHA); strings.Contains(files, "ignored.txt") {
		t.Error("ignored file was checkpointed")
	}
}

func TestCheckpointWorkingTree_CleanTreeAndNonRepoSkip(t *testing.T) {
	dir := initCheckpointRepo(t)
	cp, err := checkpointWorkingTree(context.Background(), dir, 1, true, "idle")
	if err != nil || cp != nil {
		t.Fatalf("clean tree: cp=%v err=%v, want nil/nil", cp, err)
	}
	if out, _ := exec.Command("git", "-C", dir, "show-ref").Output(); strings.Contains(string(out), "refs/sprout/") { //nolint:gosec // G204: test runs git on its temp repo
		t.Fatalf("clean tree wrote a wip ref: %s", out)
	}

	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "a.txt"), "x")
	cp, err = checkpointWorkingTree(context.Background(), plain, 1, true, "idle")
	if err != nil || cp != nil {
		t.Fatalf("non-repo: cp=%v err=%v, want nil/nil", cp, err)
	}

	cp, err = checkpointWorkingTree(context.Background(), filepath.Join(plain, "missing"), 1, true, "idle")
	if err != nil || cp != nil {
		t.Fatalf("missing dir: cp=%v err=%v, want nil/nil", cp, err)
	}
}

func TestCheckpointWorkingTree_UnbornHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	writeFile(t, filepath.Join(dir, "first.txt"), "hello\n")

	cp, err := checkpointWorkingTree(context.Background(), dir, 2, true, "drain")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if cp == nil {
		t.Fatal("unborn repo with files produced no checkpoint")
	}
	if got := gitOut(t, dir, "show", cp.WipSHA+":first.txt"); got != "hello" {
		t.Fatalf("first.txt = %q", got)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", "HEAD").Output(); err == nil { //nolint:gosec // G204: test runs git on its temp repo
		t.Fatalf("checkpoint created HEAD: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "index")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint created the repository index (stat err %v)", err)
	}
}
