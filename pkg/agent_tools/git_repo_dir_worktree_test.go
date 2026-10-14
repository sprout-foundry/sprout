package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initWorkspaceRepo creates a git repo with one commit inside t.TempDir()
// and returns its root.
func initWorkspaceRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...) //nolint:gosec // test fixture
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
	return root
}

// addLinkedWorktree creates a linked worktree of repo at a sibling path
// outside the "workspace" dir and returns the worktree root.
func addLinkedWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(repo), name)
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", name, wt) //nolint:gosec // test fixture
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", wt).Run() //nolint:gosec // test fixture
	})
	return wt
}

func TestValidateRepoDir_WorktreeOfWorkspaceRepoAllowed(t *testing.T) {
	ws := initWorkspaceRepo(t)
	wt := addLinkedWorktree(t, ws, "bugfix-wt")

	got, err := ValidateRepoDir(wt, ws)
	if err != nil {
		t.Fatalf("linked worktree of the workspace repo must be admitted, got: %v", err)
	}
	if got != wt {
		t.Errorf("resolved %q, want %q", got, wt)
	}
}

func TestValidateRepoDir_RelativeWithinWorkspace(t *testing.T) {
	ws := initWorkspaceRepo(t)
	sub := filepath.Join(ws, "pkg", "mod")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ValidateRepoDir("pkg/mod", ws)
	if err != nil {
		t.Fatalf("relative in-workspace path refused: %v", err)
	}
	if got != sub {
		t.Errorf("resolved %q, want %q", got, sub)
	}
}

func TestValidateRepoDir_UnrelatedOutsideRepoRefused(t *testing.T) {
	ws := initWorkspaceRepo(t)
	other := initWorkspaceRepo(t) // a different repo, sibling of ws's temp dir

	if _, err := ValidateRepoDir(other, ws); err == nil {
		t.Fatal("an unrelated outside repository must be refused")
	}
}

func TestValidateRepoDir_PlainOutsideDirRefused(t *testing.T) {
	ws := initWorkspaceRepo(t)
	plain := filepath.Join(filepath.Dir(ws), "not-a-repo")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateRepoDir(plain, ws); err == nil {
		t.Fatal("a plain outside directory must be refused")
	}
}

func TestValidateRepoDir_MissingPathRefused(t *testing.T) {
	ws := initWorkspaceRepo(t)
	// A worktree-shaped path that does not exist still reports not-found,
	// not a permission error.
	_, err := ValidateRepoDir(filepath.Join(filepath.Dir(ws), "ghost-wt"), ws)
	if err == nil {
		t.Fatal("missing path must be refused")
	}
}

func TestSameCommonGitDir_WorktreesShareCommonDir(t *testing.T) {
	ws := initWorkspaceRepo(t)
	wt := addLinkedWorktree(t, ws, "shared-wt")

	if !sameCommonGitDir(wt, ws) {
		t.Fatal("worktree and main checkout must share a common git dir")
	}
	other := initWorkspaceRepo(t)
	if sameCommonGitDir(other, ws) {
		t.Fatal("two different repos must not share a common git dir")
	}
	if sameCommonGitDir(t.TempDir(), ws) {
		t.Fatal("a non-repo directory must not match")
	}
}

func TestCommonGitDir_Resolves(t *testing.T) {
	ws := initWorkspaceRepo(t)
	got := commonGitDir(ws)
	if got == "" {
		t.Fatal("commonGitDir returned empty for a repo")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("common git dir does not exist: %v", err)
	}
	if commonGitDir(t.TempDir()) != "" {
		t.Error("non-repo must return empty")
	}
}
