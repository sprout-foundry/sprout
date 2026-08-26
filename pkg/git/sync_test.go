//go:build !js

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ETH-1 sync-on-resume: RunSync report tests against real temp git repos.
//
// Every fixture repo is built through newSyncTestRepo so commits carry a
// local identity (never the host's) and every git invocation is Dir-pinned
// to the fixture — see pkg/git/safety.go for why mutating commands must
// never run against the process CWD.

// syncTestRepo is a fixture working repo with an optional bare "origin".
type syncTestRepo struct {
	t      *testing.T
	dir    string // working tree
	origin string // bare origin path ("" when none)
}

// runGit runs git in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// newSyncTestRepo creates a repo with one initial commit on branch main.
func newSyncTestRepo(t *testing.T) *syncTestRepo {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	writeSyncFile(t, dir, "README.md", "hello\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")
	return &syncTestRepo{t: t, dir: dir}
}

// withOrigin adds a bare origin, pushes main, and sets the upstream.
func (r *syncTestRepo) withOrigin() *syncTestRepo {
	r.t.Helper()
	origin := filepath.Join(r.t.TempDir(), "origin.git")
	runGit(r.t, r.dir, "init", "--bare", "-b", "main", origin)
	runGit(r.t, r.dir, "remote", "add", "origin", origin)
	runGit(r.t, r.dir, "push", "-u", "origin", "main")
	r.origin = origin
	return r
}

// clone makes a second working repo tracking r as origin.
func (r *syncTestRepo) clone(t *testing.T) *syncTestRepo {
	t.Helper()
	dir := t.TempDir()
	runGit(t, r.dir, "clone", r.origin, dir)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	return &syncTestRepo{t: t, dir: dir, origin: r.origin}
}

// commitFile writes path, stages and commits it.
func (r *syncTestRepo) commitFile(path, content, msg string) {
	r.t.Helper()
	writeSyncFile(r.t, r.dir, path, content)
	runGit(r.t, r.dir, "add", "--", path)
	runGit(r.t, r.dir, "commit", "-m", msg)
}

func writeSyncFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// headOf returns the full HEAD sha of a repo dir.
func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestRunSync_CleanRepo(t *testing.T) {
	repo := newSyncTestRepo(t)
	report, err := RunSync(context.Background(), repo.dir, false)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if !report.InGitRepo {
		t.Fatal("expected in_git_repo true")
	}
	if report.Branch != "main" {
		t.Fatalf("branch = %q, want main", report.Branch)
	}
	if len(report.DirtyFiles) != 0 {
		t.Fatalf("dirty_files = %v, want empty", report.DirtyFiles)
	}
	if report.LastCommit.Sha == "" || report.LastCommit.Subject != "initial commit" {
		t.Fatalf("last_commit = %+v", report.LastCommit)
	}
	if report.LastCommit.Author != "Test User" {
		t.Fatalf("author = %q", report.LastCommit.Author)
	}
	if report.Pull.Result != SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted", report.Pull.Result)
	}
}

func TestRunSync_NotARepo(t *testing.T) {
	report, err := RunSync(context.Background(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("not-a-repo must be reportable, got error: %v", err)
	}
	if report.InGitRepo {
		t.Fatal("expected in_git_repo false")
	}
	if report.Pull.Result != SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted", report.Pull.Result)
	}
	if report.DirtyFiles == nil || len(report.DirtyFiles) != 0 {
		t.Fatalf("dirty_files = %v, want empty non-nil", report.DirtyFiles)
	}
	if report.LastCommit != (SyncLastCommit{}) {
		t.Fatalf("last_commit = %+v, want zero", report.LastCommit)
	}
}

// TestRunSync_NotARepoUnderLocalizedEnv is the regression test for the
// locale-dependent stderr sniffing in detectSyncGitRepo: git translates its
// own fatal messages ("fatal: not a git repository…" → "fatal: kein
// Git-Repository …") when the caller's locale is not C, which used to turn
// the reportable not-a-repo state into a catastrophic error. runSyncGit
// pins LC_ALL=C/LANG=C on every git invocation, so this must still report
// in_git_repo=false with a nil error regardless of the ambient locale.
func TestRunSync_NotARepoUnderLocalizedEnv(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LC_MESSAGES", "de_DE.UTF-8")

	report, err := RunSync(context.Background(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("localized not-a-repo must stay reportable, got error: %v", err)
	}
	if report.InGitRepo {
		t.Fatal("expected in_git_repo false")
	}
	if report.Pull.Result != SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted", report.Pull.Result)
	}
	if report.DirtyFiles == nil || len(report.DirtyFiles) != 0 {
		t.Fatalf("dirty_files = %v, want empty non-nil", report.DirtyFiles)
	}
}

// TestRunSyncGitPinsLocale deterministically proves the env pinning the test
// above relies on: a localized parent environment must be shadowed by
// LC_ALL=C / LANG=C on every git invocation runSyncGit spawns. It reads back
// the locale git actually received rather than depending on a de_DE locale
// (or a translated git build) being installed on the host.
func TestRunSyncGitPinsLocale(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")

	dir := t.TempDir()
	// A `!` shell alias runs inside git's own environment, so it reports
	// the locale runSyncGit chose for the subprocess.
	out, err := runSyncGit(context.Background(), dir, "-c", "alias.plocale=!printf '%s\\n%s\\n' \"$LC_ALL\" \"$LANG\"", "plocale")
	if err != nil {
		t.Fatalf("runSyncGit probe: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(out.stdout))
	if len(lines) != 2 {
		t.Fatalf("probe output = %q, want LC_ALL and LANG lines", out.stdout)
	}
	if lines[0] != "C" || lines[1] != "C" {
		t.Fatalf("git saw LC_ALL=%q LANG=%q, want C/C (inherited locale must be shadowed)", lines[0], lines[1])
	}
}

func TestRunSync_DirtyFilesIncludeUntrackedAndRenamed(t *testing.T) {
	repo := newSyncTestRepo(t)
	// Untracked file.
	writeSyncFile(t, repo.dir, "notes.txt", "uncommitted\n")
	// Modified tracked file.
	writeSyncFile(t, repo.dir, "README.md", "changed\n")
	// Renamed tracked file (staged rename).
	writeSyncFile(t, repo.dir, "old.go", "package x\n")
	runGit(t, repo.dir, "add", "--", "old.go")
	runGit(t, repo.dir, "commit", "-m", "add old.go")
	runGit(t, repo.dir, "mv", "old.go", "new.go")

	report, err := RunSync(context.Background(), repo.dir, false)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	want := []string{"README.md", "new.go", "notes.txt"} // sorted porcelain order
	if got := report.DirtyFiles; len(got) != 3 {
		t.Fatalf("dirty_files = %v, want %v", got, want)
	}
	set := map[string]bool{}
	for _, f := range report.DirtyFiles {
		set[f] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Fatalf("dirty_files %v missing %q", report.DirtyFiles, w)
		}
	}
	if set["old.go"] {
		t.Fatalf("renamed-away path old.go must not be listed: %v", report.DirtyFiles)
	}
}

func TestRunSync_NoUpstreamSkipsPull(t *testing.T) {
	repo := newSyncTestRepo(t)
	report, err := RunSync(context.Background(), repo.dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullSkippedNoUpstream {
		t.Fatalf("pull.result = %q, want skipped_no_upstream", report.Pull.Result)
	}
	if report.Pull.Attempted {
		t.Fatal("pull.attempted must be false for skipped_no_upstream")
	}
	if report.Ahead != 0 || report.Behind != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 0/0 without upstream", report.Ahead, report.Behind)
	}
}

func TestRunSync_DirtyTrackedSkipsPull(t *testing.T) {
	repo := newSyncTestRepo(t).withOrigin()
	writeSyncFile(t, repo.dir, "README.md", "dirty edit\n")
	dirtyBefore, _ := os.ReadFile(filepath.Join(repo.dir, "README.md"))

	report, err := RunSync(context.Background(), repo.dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullSkippedDirty {
		t.Fatalf("pull.result = %q, want skipped_dirty", report.Pull.Result)
	}
	if report.Pull.Attempted {
		t.Fatal("pull.attempted must be false for skipped_dirty")
	}
	// Non-destructive proof: the dirty file survives untouched.
	dirtyAfter, _ := os.ReadFile(filepath.Join(repo.dir, "README.md"))
	if string(dirtyBefore) != string(dirtyAfter) {
		t.Fatal("skipped_dirty must not modify the working tree")
	}
}

func TestRunSync_AheadBehindCounts(t *testing.T) {
	origin := newSyncTestRepo(t).withOrigin()
	a := origin.clone(t)

	// 'a' ahead by 2 (commits not pushed).
	a.commitFile("f1.txt", "1\n", "a1")
	a.commitFile("f2.txt", "2\n", "a2")

	// origin moves ahead by 3 via the original repo.
	origin.commitFile("g1.txt", "1\n", "o1")
	origin.commitFile("g2.txt", "2\n", "o2")
	origin.commitFile("g3.txt", "3\n", "o3")
	runGit(t, origin.dir, "push", "origin", "main")

	// 'a' needs a fetch to see origin's new commits; RunSync with
	// attemptPull=false performs NO network access, so counts reflect the
	// last fetch. Fetch explicitly, then measure.
	runGit(t, a.dir, "fetch", "origin")

	report, err := RunSync(context.Background(), a.dir, false)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Behind != 3 {
		t.Fatalf("behind = %d, want 3", report.Behind)
	}
	if report.Ahead != 2 {
		t.Fatalf("ahead = %d, want 2", report.Ahead)
	}
}

func TestRunSync_UntrackedOnlyPullFastForwards(t *testing.T) {
	origin := newSyncTestRepo(t).withOrigin()
	a := origin.clone(t)

	// Origin advances; 'a' has only an UNTRACKED file locally.
	origin.commitFile("new.txt", "from origin\n", "origin advance")
	runGit(t, origin.dir, "push", "origin", "main")
	writeSyncFile(t, a.dir, "local.txt", "untracked\n")

	headBefore := headOf(t, a.dir)
	report, err := RunSync(context.Background(), a.dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullFastForwarded {
		t.Fatalf("pull.result = %q, want fast_forwarded (untracked-only must not block)", report.Pull.Result)
	}
	if !report.Pull.Attempted {
		t.Fatal("pull.attempted must be true for fast_forwarded")
	}
	if headOf(t, a.dir) == headBefore {
		t.Fatal("expected HEAD to move after fast-forward")
	}
	// The untracked file survives.
	if _, err := os.Stat(filepath.Join(a.dir, "local.txt")); err != nil {
		t.Fatalf("untracked file must survive the pull: %v", err)
	}
	// last_commit now describes origin's tip.
	if report.LastCommit.Subject != "origin advance" {
		t.Fatalf("last_commit.subject = %q, want 'origin advance'", report.LastCommit.Subject)
	}
	// After the fast-forward, behind drops to 0.
	if report.Behind != 0 {
		t.Fatalf("behind after ff = %d, want 0", report.Behind)
	}
}

func TestRunSync_UpToDatePull(t *testing.T) {
	repo := newSyncTestRepo(t).withOrigin()
	report, err := RunSync(context.Background(), repo.dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullUpToDate {
		t.Fatalf("pull.result = %q, want up_to_date", report.Pull.Result)
	}
}

func TestRunSync_DivergedPullReportsError(t *testing.T) {
	origin := newSyncTestRepo(t).withOrigin()
	a := origin.clone(t)

	// Both sides commit → histories diverge.
	origin.commitFile("o.txt", "o\n", "origin side")
	runGit(t, origin.dir, "push", "origin", "main")
	a.commitFile("a.txt", "a\n", "a side")

	headBefore := headOf(t, a.dir)
	report, err := RunSync(context.Background(), a.dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullError {
		t.Fatalf("pull.result = %q, want error on diverged history", report.Pull.Result)
	}
	if report.Pull.Error == "" {
		t.Fatal("pull.error must carry git's refusal message")
	}
	// Non-destructive proof: HEAD unchanged, local commit intact.
	if headOf(t, a.dir) != headBefore {
		t.Fatal("diverged pull must not move HEAD")
	}
	if report.Ahead != 1 || report.Behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, want 1/1 after fetch", report.Ahead, report.Behind)
	}
}

func TestRunSync_AttemptPullFalseNeverTouchesRemote(t *testing.T) {
	origin := newSyncTestRepo(t).withOrigin()
	a := origin.clone(t)

	// Origin advances behind 'a's back.
	origin.commitFile("behind.txt", "x\n", "origin moved")
	runGit(t, origin.dir, "push", "origin", "main")

	remoteTip := headOf(t, origin.origin) // bare repo HEAD tracks main

	report, err := RunSync(context.Background(), a.dir, false)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if report.Pull.Result != SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted", report.Pull.Result)
	}
	// The remote-tracking ref in 'a' must not have moved (no fetch ran).
	// Ground truth: 'a' still sees itself as up to date with its stale
	// tracking ref, so behind must be 0.
	if report.Behind != 0 {
		t.Fatalf("behind = %d with attemptPull=false — a fetch must have run (bug)", report.Behind)
	}
	_ = remoteTip
}

func TestRunSync_EmptyRepoNoCommits(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	report, err := RunSync(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if !report.InGitRepo {
		t.Fatal("expected in_git_repo true for a commit-less repo")
	}
	if report.LastCommit != (SyncLastCommit{}) {
		t.Fatalf("last_commit = %+v, want zero struct", report.LastCommit)
	}
	if report.Pull.Result != SyncPullSkippedNoUpstream {
		t.Fatalf("pull.result = %q, want skipped_no_upstream", report.Pull.Result)
	}
}

func TestParseStatusPorcelain(t *testing.T) {
	// -z records: XY <path> NUL, renames carry the original path as the
	// next NUL field.
	z := "M  a.go\x00R  b.go\x00old-b.go\x00?? c.txt\x00 D d.go\x00"
	files, hasTracked := parseStatusPorcelain(z)
	want := []string{"a.go", "b.go", "c.txt", "d.go"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i, w := range want {
		if files[i] != w {
			t.Fatalf("files[%d] = %q, want %q", i, files[i], w)
		}
	}
	if !hasTracked {
		t.Fatal("tracked-dirty flag must be true with M/R/D entries")
	}
}

func TestParseRevListCount(t *testing.T) {
	if b, a := parseRevListCount("3\t2\n"); b != 3 || a != 2 {
		t.Fatalf("got behind=%d ahead=%d, want 3/2", b, a)
	}
	if b, a := parseRevListCount("garbage"); b != 0 || a != 0 {
		t.Fatalf("garbage must parse as 0/0, got %d/%d", b, a)
	}
}
