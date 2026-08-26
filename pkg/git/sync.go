// ETH-1 "sync-on-resume": reconcile the git state of a workspace container
// and report it as a single JSON object.
//
// The report is consumed by the platform (sprout-foundry) when a stopped
// workspace machine is resumed, and by the browser tab through the daemon's
// GET /api/sync endpoint and the "sync" field of /api/bootstrap. The JSON
// shape below is a PINNED CONTRACT — field names, nesting and zero-values
// must not change without coordinating with the platform parser.
//
// Everything here is intentionally dependency-free: it shells out to git,
// touches no configuration, no LLM and no agent state, so the same entry
// point serves the CLI (`sprout sync`) and the daemon (running in-container
// as root against /work) with identical output.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Pull result values (pull.result in the JSON contract).
const (
	// SyncPullNotAttempted is used when no pull was requested at all
	// (attemptPull=false, or the directory is not a git repository).
	SyncPullNotAttempted = "not_attempted"
	// SyncPullUpToDate: `git pull --ff-only` ran and HEAD already matched
	// the upstream.
	SyncPullUpToDate = "up_to_date"
	// SyncPullFastForwarded: `git pull --ff-only` ran and moved HEAD
	// forward to the upstream.
	SyncPullFastForwarded = "fast_forwarded"
	// SyncPullSkippedNoUpstream: pull was requested but the branch has no
	// @{upstream} configured.
	SyncPullSkippedNoUpstream = "skipped_no_upstream"
	// SyncPullSkippedDirty: pull was requested but tracked files have
	// uncommitted changes — dirty work is never put at risk.
	SyncPullSkippedDirty = "skipped_dirty"
	// SyncPullError: git refused the pull (diverged history, an untracked
	// file would be clobbered, timeout, …). git's own message is preserved
	// in SyncPull.Error; we never work around a refusal.
	SyncPullError = "error"
)

// SyncGitTimeout bounds each individual git invocation. It is a var (not a
// const) so tests — or a caller with unusual latency requirements — can
// tighten or relax it.
var SyncGitTimeout = 10 * time.Second

// syncPullErrorMax bounds pull.error so a pathological git message cannot
// balloon the report.
const syncPullErrorMax = 2000

// SyncLastCommit is the "last_commit" object of the report. Empty object
// when the repository has no commits (it is a value type, never null).
type SyncLastCommit struct {
	Sha       string `json:"sha"`
	Subject   string `json:"subject"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
}

// SyncPull is the "pull" object of the report.
//
// Attempted is true iff a `git pull --ff-only` subprocess actually ran;
// every skipped_* outcome therefore reports Attempted=false, and only
// up_to_date / fast_forwarded / error report Attempted=true.
type SyncPull struct {
	Attempted bool   `json:"attempted"`
	Result    string `json:"result"`
	Error     string `json:"error"`
}

// SyncReport is the pinned ETH-1 JSON contract:
//
//	{
//	  "in_git_repo": true, "branch": "main",
//	  "dirty_files": ["cmd/foo.go", "notes.txt"],
//	  "ahead": 1, "behind": 2,
//	  "last_commit": {"sha": "...", "subject": "...", "author": "...", "timestamp": "..."},
//	  "pull": {"attempted": true, "result": "fast_forwarded", "error": ""}
//	}
type SyncReport struct {
	InGitRepo  bool           `json:"in_git_repo"`
	Branch     string         `json:"branch"`
	DirtyFiles []string       `json:"dirty_files"`
	Ahead      int            `json:"ahead"`
	Behind     int            `json:"behind"`
	LastCommit SyncLastCommit `json:"last_commit"`
	Pull       SyncPull       `json:"pull"`
}

// newSyncReport returns the zero-state report: an empty (never null)
// dirty_files list and a "not_attempted" pull.
func newSyncReport() SyncReport {
	return SyncReport{
		DirtyFiles: []string{},
		Pull:       SyncPull{Result: SyncPullNotAttempted},
	}
}

// RunSync inspects the git repository containing repoDir and returns the
// ETH-1 reconciliation report.
//
// attemptPull=true additionally attempts a non-destructive `git pull
// --ff-only`. The fetch is implicit in that pull: when attemptPull is false
// NO network/remote access happens and ahead/behind reflect the last-known
// remote-tracking state. Pull is attempted only when the branch has an
// upstream AND no tracked file is dirty (untracked-only is allowed — git
// itself refuses a pull that would clobber them, and that refusal is
// reported as result "error" with git's message rather than worked around).
//
// NEVER destructive: no reset, checkout, stash, clean or force anything.
//
// Error handling follows the contract: "not a git repository" is a
// reportable state (InGitRepo=false, zeroed fields, nil error); only
// catastrophic failures (unreadable directory, git binary failure, corrupt
// index) return a non-nil error. Field-level git failures degrade to
// zero values instead of failing the whole report.
func RunSync(ctx context.Context, repoDir string, attemptPull bool) (SyncReport, error) {
	report := newSyncReport()

	dir, err := resolveSyncDir(repoDir)
	if err != nil {
		return report, err
	}

	// 1. Is this a git repository at all?
	inRepo, err := detectSyncGitRepo(ctx, dir)
	if err != nil {
		return report, err
	}
	if !inRepo {
		// Reportable state, not an error: every other field stays
		// zero/empty and the pull stays "not_attempted".
		return report, nil
	}
	report.InGitRepo = true

	// 2. Branch name. Cosmetic on failure (detached HEAD → "HEAD").
	report.Branch = syncBranch(ctx, dir)

	// 3. Dirty files (pre-pull: the tracked-dirty subset gates the pull).
	dirtyFiles, hasDirtyTracked, err := collectDirtyFiles(ctx, dir)
	if err != nil {
		return report, err
	}
	report.DirtyFiles = dirtyFiles

	upstream, hasUpstream := syncUpstream(ctx, dir)

	// 4. Optional pull. Ordering matters: the pull's implicit fetch updates
	// the remote-tracking refs, so ahead/behind and last_commit are read
	// AFTER it to describe the reconciled state rather than the stale one.
	if attemptPull {
		switch {
		case !hasUpstream:
			report.Pull.Result = SyncPullSkippedNoUpstream
		case hasDirtyTracked:
			report.Pull.Result = SyncPullSkippedDirty
		default:
			report.Pull = syncPullFFOnly(ctx, dir)
		}
	}

	// 5. Post-pull state. A fast-forward rewrote the working tree, so the
	// dirty list is recomputed; on any other outcome the pre-pull list
	// still describes the tree exactly.
	if report.Pull.Result == SyncPullFastForwarded {
		if dirty, _, err := collectDirtyFiles(ctx, dir); err == nil {
			report.DirtyFiles = dirty
		}
	}
	if hasUpstream {
		report.Behind, report.Ahead = syncAheadBehind(ctx, dir, upstream)
	}
	report.LastCommit = syncLastCommit(ctx, dir)

	return report, nil
}

// resolveSyncDir normalizes the working directory for every git invocation.
// It is never empty: an empty dir would send mutating commands (pull) at the
// process CWD — the exact hazard pkg/git/safety.go guards against.
func resolveSyncDir(repoDir string) (string, error) {
	if strings.TrimSpace(repoDir) != "" {
		return repoDir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("sync: no repository directory given and failed to resolve working directory: %w", err)
	}
	return cwd, nil
}

// syncGitOutput captures stdout/stderr separately so error reporting can
// quote git's own message (stderr first, stdout as fallback).
type syncGitOutput struct {
	stdout string
	stderr string
}

// syncGitError wraps a failed git invocation and renders git's own message
// rather than a generic wrapper — the contract requires the pull refusal
// text to be surfaced verbatim.
type syncGitError struct {
	args []string
	out  syncGitOutput
	err  error
}

func (e *syncGitError) Error() string {
	msg := strings.TrimSpace(e.out.stderr)
	if msg == "" {
		msg = strings.TrimSpace(e.out.stdout)
	}
	if msg == "" {
		return fmt.Sprintf("git %s failed: %v", strings.Join(e.args, " "), e.err)
	}
	return msg
}

// runSyncGit executes a single git command in dir with a bounded timeout.
// The timeout (and any parent-context cancellation) is reported as a plain
// error rather than a *syncGitError, because there is no git message to quote.
func runSyncGit(ctx context.Context, dir string, args ...string) (syncGitOutput, error) {
	if dir == "" {
		return syncGitOutput{}, errors.New("git working directory must not be empty")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, SyncGitTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = dir
	// Unattended safety: never block on an interactive credential prompt.
	// A remote needing auth fails fast (reported as pull "error") instead of
	// hanging the daemon until the timeout kills it.
	//
	// Locale pinning: repo detection and the fast-forward sniff parse git's
	// stderr MESSAGES, which git translates when the caller's locale is not
	// C (e.g. LANG=de_DE.UTF-8 renders "fatal: kein Git-Repository …").
	// LC_ALL overrides every other locale variable, and LANG is the
	// fallback; appending both shadows any inherited localized value.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"LANG=C",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	out := syncGitOutput{stdout: stdout.String(), stderr: stderr.String()}
	if cmdCtx.Err() != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(args, " "), cmdCtx.Err())
	}
	if err != nil {
		return out, &syncGitError{args: args, out: out, err: err}
	}
	return out, nil
}

// detectSyncGitRepo reports whether dir is inside a git work tree. A plain
// "not a git repository" is reported as (false, nil); anything else (missing
// directory, unreadable path, broken git) is a catastrophic error.
func detectSyncGitRepo(ctx context.Context, dir string) (bool, error) {
	out, err := runSyncGit(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err == nil {
		return strings.TrimSpace(out.stdout) == "true", nil
	}
	var gitErr *syncGitError
	// Locale-sensitive message match: safe only because runSyncGit pins
	// LC_ALL=C, so git never translates its own fatal messages.
	if errors.As(err, &gitErr) && strings.Contains(strings.ToLower(gitErr.Error()), "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("sync: %s: %w", dir, err)
}

// syncBranch returns the checked-out branch name. `git branch
// --show-current` is empty for detached HEAD, where the symbolic
// abbreviated HEAD ("HEAD") is used instead. Failure degrades to "".
func syncBranch(ctx context.Context, dir string) string {
	if out, err := runSyncGit(ctx, dir, "branch", "--show-current"); err == nil {
		if name := strings.TrimSpace(out.stdout); name != "" {
			return name
		}
	}
	if out, err := runSyncGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		return strings.TrimSpace(out.stdout)
	}
	return ""
}

// collectDirtyFiles runs `git status --porcelain -z` once and returns every
// path whose content differs from HEAD or does not exist in HEAD — modified,
// staged, deleted, renamed, copied AND untracked (untracked directories are
// expanded, hence --untracked-files=all). Paths are repo-root relative.
//
// hasDirtyTracked excludes untracked entries: it is the "is there
// uncommitted work that a pull could clobber" predicate used to gate the
// pull.
func collectDirtyFiles(ctx context.Context, dir string) (files []string, hasDirtyTracked bool, err error) {
	out, err := runSyncGit(ctx, dir, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, false, fmt.Errorf("sync: git status failed: %w", err)
	}
	files, hasDirtyTracked = parseStatusPorcelain(out.stdout)
	return files, hasDirtyTracked, nil
}

// parseStatusPorcelain parses NUL-terminated `git status --porcelain -z`
// output. Rename/copy entries occupy two NUL-terminated fields (new path,
// then original path); the original is consumed so it is not mistaken for an
// entry of its own.
func parseStatusPorcelain(zOutput string) (files []string, hasDirtyTracked bool) {
	files = []string{}
	if zOutput == "" {
		return files, false
	}
	records := strings.Split(zOutput, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < 4 {
			continue // empty tail or malformed entry
		}
		x, y := rec[0], rec[1]
		path := rec[3:]
		if x == '?' && y == '?' {
			files = append(files, path)
			continue // untracked: differs from HEAD by definition, but is not "tracked dirty"
		}
		hasDirtyTracked = true
		files = append(files, path)
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // skip the original-path field of a rename/copy pair
		}
	}
	return files, hasDirtyTracked
}

// syncUpstream resolves the @{upstream} short name. Failure means "no
// upstream configured" (0/0 ahead-behind, pull skipped) and is not an error.
func syncUpstream(ctx context.Context, dir string) (string, bool) {
	out, err := runSyncGit(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(out.stdout)
	if name == "" || name == "HEAD" {
		return "", false
	}
	return name, true
}

// syncAheadBehind counts divergence from the upstream.
//
//	`git rev-list --left-right --count @{upstream}...HEAD`
//
// prints "<behind>\t<ahead>": the LEFT side (@{upstream}) count is how many
// commits we are behind, the RIGHT side (HEAD) count is how many we are
// ahead. Any failure degrades to 0/0 rather than failing the report.
func syncAheadBehind(ctx context.Context, dir, upstream string) (behind, ahead int) {
	out, err := runSyncGit(ctx, dir, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0
	}
	return parseRevListCount(out.stdout)
}

// parseRevListCount parses "behind\tahead" (whitespace-tolerant). Anything
// unexpected yields 0/0.
func parseRevListCount(out string) (behind, ahead int) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0
	}
	b, errB := strconv.Atoi(fields[0])
	a, errA := strconv.Atoi(fields[1])
	if errB != nil || errA != nil {
		return 0, 0
	}
	return b, a
}

// syncLastCommit reads the tip commit. No commits yet (or an unreadable
// history) yields the empty object, per the contract.
func syncLastCommit(ctx context.Context, dir string) SyncLastCommit {
	// %x1f (unit separator) delimits the fields so a subject containing
	// punctuation can never shift the parse.
	out, err := runSyncGit(ctx, dir, "log", "-1", "--format=%h%x1f%s%x1f%an%x1f%aI")
	if err != nil {
		return SyncLastCommit{}
	}
	fields := strings.SplitN(strings.TrimRight(out.stdout, "\n"), "\x1f", 4)
	if len(fields) != 4 {
		return SyncLastCommit{}
	}
	return SyncLastCommit{
		Sha:       fields[0],
		Subject:   fields[1],
		Author:    fields[2],
		Timestamp: normalizeGitTimestamp(fields[3]),
	}
}

// normalizeGitTimestamp converts git's strict ISO 8601 author date to an
// RFC3339 UTC timestamp ("2026-08-25T22:14:03Z"), matching the pinned
// example. Unparseable input is passed through untouched.
func normalizeGitTimestamp(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return raw
}

// syncPullFFOnly attempts the non-destructive pull.
//
// Interaction with pull.rebase user config is version-dependent — NOT the
// blanket override the earlier comment claimed:
//
//   - git ≥ 2.35: "ff-only takes precedence over rebase" — --ff-only turns
//     rebase off, so a divergent history dies with "Not possible to
//     fast-forward, aborting." and the only outcomes are fast-forward or
//     refuse. (pull.rebase=true still makes git demand a clean tree first —
//     "cannot pull with rebase: You have unstaged changes." — but the
//     hasDirtyTracked gate in RunSync means we never ask from a dirty tree.)
//   - git < 2.35: --ff-only does NOT disable rebase. With pull.rebase=true
//     and a diverged history git would REBASE (rewriting local commits)
//     instead of refusing. No such refusal/rewrite is possible from this
//     call site in practice, because RunSync only attempts a pull when the
//     tree has no dirty tracked files and git's own descendant check still
//     fast-forwards when it can — but the guarantee comes from those gates,
//     not from --ff-only itself.
//
// pull.autostash is inert without --rebase, so it cannot resurrect a
// refused pull either.
func syncPullFFOnly(ctx context.Context, dir string) SyncPull {
	pull := SyncPull{Attempted: true, Result: SyncPullError}

	before := syncHead(ctx, dir)
	out, err := runSyncGit(ctx, dir, "pull", "--ff-only")
	if err != nil {
		// git refused (diverged, would clobber untracked files, auth,
		// timeout, …). Never work around it — surface git's message.
		pull.Error = truncateSyncMessage(err.Error())
		return pull
	}

	// HEAD movement is locale-independent proof of a fast-forward; the
	// "Fast-forward" string sniff below is locale-dependent, which is why
	// runSyncGit pins LC_ALL=C. It is only a fallback for when HEAD is
	// unreadable (an unborn branch, no commits).
	after := syncHead(ctx, dir)
	if (before != "" && after != "" && before != after) || strings.Contains(out.stdout, "Fast-forward") {
		pull.Result = SyncPullFastForwarded
		return pull
	}
	pull.Result = SyncPullUpToDate
	return pull
}

// syncHead returns the full HEAD sha, or "" when HEAD does not resolve
// (unborn branch) or fails.
func syncHead(ctx context.Context, dir string) string {
	out, err := runSyncGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.stdout)
}

// truncateSyncMessage bounds an error string carried into the report.
func truncateSyncMessage(msg string) string {
	if len(msg) <= syncPullErrorMax {
		return msg
	}
	return msg[:syncPullErrorMax] + "… (truncated)"
}
