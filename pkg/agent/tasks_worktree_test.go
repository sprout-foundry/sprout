package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTaskTestRepo creates a git repository with one commit and returns its
// path. The repo is hermetic: user identity comes from the environment git
// config, never the developer's global config.
func initTaskTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-b", "main")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestCreateTaskWorktree_CreatesBranchAndPath(t *testing.T) {
	repo := initTaskTestRepo(t)

	ws, err := createTaskWorktree(context.Background(), repo, "feat/add-login", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { ws.remove(context.Background()) })

	wantPath := filepath.Join(repo, ".sprout", "tasks", "add-login")
	if ws.Path != wantPath {
		t.Errorf("Path = %q, want %q", ws.Path, wantPath)
	}
	if ws.Base != "main" {
		t.Errorf("Base = %q, want the current branch", ws.Base)
	}
	if _, err := os.Stat(filepath.Join(wantPath, ".git")); err != nil {
		t.Errorf("worktree .git missing at %s: %v", wantPath, err)
	}

	out, err := runGitIn(context.Background(), ws.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatalf("read worktree branch: %v", err)
	}
	if got := strings.TrimSpace(out); got != "feat/add-login" {
		t.Errorf("worktree branch = %q, want feat/add-login", got)
	}
}

func TestCreateTaskWorktree_RefusesDetachedHead(t *testing.T) {
	repo := initTaskTestRepo(t)
	if _, err := runGitIn(context.Background(), repo, "checkout", "--detach"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	_, err := createTaskWorktree(context.Background(), repo, "feat/x", "")
	if err == nil {
		t.Fatal("expected an error on a detached HEAD")
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("error = %v, want it to name the detached HEAD", err)
	}
}

func TestCreateTaskWorktree_RefusesExistingPath(t *testing.T) {
	repo := initTaskTestRepo(t)

	first, err := createTaskWorktree(context.Background(), repo, "feat/dup", "")
	if err != nil {
		t.Fatalf("first createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { first.remove(context.Background()) })

	if _, err := createTaskWorktree(context.Background(), repo, "fix/dup", ""); err == nil {
		t.Fatal("expected an error when the worktree path already exists")
	}
}

func TestCreateTaskWorktree_HonoursExplicitBase(t *testing.T) {
	repo := initTaskTestRepo(t)
	if _, err := runGitIn(context.Background(), repo, "commit", "--allow-empty", "-m", "second"); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	head, err := runGitIn(context.Background(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	first, err := runGitIn(context.Background(), repo, "rev-parse", "HEAD~1")
	if err != nil {
		t.Fatalf("rev-parse HEAD~1: %v", err)
	}

	ws, err := createTaskWorktree(context.Background(), repo, "feat/from-base", strings.TrimSpace(first))
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { ws.remove(context.Background()) })

	got, err := runGitIn(context.Background(), ws.Path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse worktree HEAD: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(first) {
		t.Errorf("worktree HEAD = %s, want the explicit base %s (not %s)", strings.TrimSpace(got), strings.TrimSpace(first), strings.TrimSpace(head))
	}
}

func TestCreateTaskWorktree_RefusesNonRepo(t *testing.T) {
	if _, err := createTaskWorktree(context.Background(), t.TempDir(), "feat/x", ""); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

func TestRemoveTaskWorktree_KeepsBranch(t *testing.T) {
	repo := initTaskTestRepo(t)
	ws, err := createTaskWorktree(context.Background(), repo, "feat/keep-branch", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}

	ws.remove(context.Background())

	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Errorf("worktree path still exists after remove: %v", err)
	}
	if _, err := runGitIn(context.Background(), repo, "rev-parse", "--verify", "--quiet", "feat/keep-branch"); err != nil {
		t.Errorf("branch was deleted with the worktree: %v", err)
	}
}

func TestParseTaskBranchName(t *testing.T) {
	cases := []struct {
		name   string
		reply  string
		want   string
		wantOK bool
	}{
		{"bare", "feat/add-login", "feat/add-login", true},
		{"quoted", "\"feat/add-login\"", "feat/add-login", true},
		{"backticked", "`fix/null-pointer`", "fix/null-pointer", true},
		{"sentence", "The branch should be `refactor/split-runner`.", "", false},
		{"multiline", "Sure:\nfeat/add-login\n", "feat/add-login", true},
		{"uppercase is normalized", "Feat/Add-Login", "feat/add-login", true},
		{"no type prefix", "add-login", "", false},
		{"unknown type", "wip/add-login", "", false},
		{"spaces in slug", "feat/add login", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTaskBranchName(tc.reply)
			if ok != tc.wantOK {
				t.Fatalf("parseTaskBranchName(%q) ok = %v, want %v (got %q)", tc.reply, ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("parseTaskBranchName(%q) = %q, want %q", tc.reply, got, tc.want)
			}
		})
	}
}

func TestParseTaskBranchName_TruncatesAtWordBoundary(t *testing.T) {
	long := "feat/" + strings.Repeat("word-", 20) + "end"
	got, ok := parseTaskBranchName(long)
	if !ok {
		t.Fatalf("expected a long valid branch to be accepted, got %q", got)
	}
	if len(got) > taskBranchMaxLen {
		t.Errorf("branch %q is %d chars, over the %d cap", got, len(got), taskBranchMaxLen)
	}
	if !taskBranchPattern.MatchString(got) {
		t.Errorf("truncated branch %q is not a valid task branch", got)
	}
}

func TestFallbackTaskSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Add login page", "add-login-page"},
		{"  Fix: the NULL pointer!!  ", "fix-the-null-pointer"},
		{"", "task"},
		{"!!!", "task"},
		{"one two three four five six seven eight", "one-two-three-four-five-six"},
	}
	for _, tc := range cases {
		if got := fallbackTaskSlug(tc.in); got != tc.want {
			t.Errorf("fallbackTaskSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFallbackTaskSlug_IsAlwaysValidBranch(t *testing.T) {
	for _, in := range []string{"", "!!!", strings.Repeat("verylongword", 20)} {
		branch := "feat/" + fallbackTaskSlug(in)
		if _, ok := normalizeTaskBranch(branch); !ok {
			t.Errorf("fallback branch %q for input %q is not valid", branch, in)
		}
	}
}

func TestSuggestTaskBranch_FallsBackWithoutAgent(t *testing.T) {
	got, err := suggestTaskBranch(context.Background(), nil, "Add login page")
	if err != nil {
		t.Fatalf("suggestTaskBranch: %v", err)
	}
	if got != "feat/add-login-page" {
		t.Errorf("got %q, want the deterministic fallback", got)
	}
}

func TestReadWorkspaceConventions(t *testing.T) {
	dir := t.TempDir()
	if got := readWorkspaceConventions(dir); got != "" {
		t.Errorf("expected empty conventions, got %q", got)
	}
	body := "# Conventions\n\nUse feat/ and fix/ prefixes.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readWorkspaceConventions(dir); !strings.Contains(got, "feat/ and fix/") {
		t.Errorf("conventions = %q, want the AGENTS.md body", got)
	}
}

func TestReadWorkspaceConventions_CapsSize(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", taskConventionsBudget*2)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readWorkspaceConventions(dir); len(got) != taskConventionsBudget {
		t.Errorf("conventions length = %d, want the %d cap", len(got), taskConventionsBudget)
	}
}
