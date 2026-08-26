package txn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TxnGitTimeout bounds each individual git invocation BuildStatus/BuildPull
// makes. It is a var (not a const) so tests can tighten or relax it.
var TxnGitTimeout = 10 * time.Second

// errEscapesRoot marks a path whose symlink resolution left the workdir.
var errEscapesRoot = errors.New("path resolves outside the workdir")

// txnGitOutput captures stdout/stderr separately so error reporting can
// quote git's own message (stderr first, stdout as fallback).
type txnGitOutput struct {
	stdout string
	stderr string
}

// txnGitError wraps a failed git invocation and renders git's own message
// rather than a generic wrapper, mirroring pkg/git's syncGitError.
type txnGitError struct {
	args []string
	out  txnGitOutput
	err  error
}

func (e *txnGitError) Error() string {
	msg := strings.TrimSpace(e.out.stderr)
	if msg == "" {
		msg = strings.TrimSpace(e.out.stdout)
	}
	if msg == "" {
		return fmt.Sprintf("git %s failed: %v", strings.Join(e.args, " "), e.err)
	}
	return msg
}

// runTxnGit executes a single git command in dir with a bounded timeout.
// Locale pinning matters for the "not a git repository" message sniff in
// detectTxnGitRepo: LC_ALL overrides every other locale variable, and LANG
// is the fallback; appending both shadows any inherited localized value.
func runTxnGit(ctx context.Context, dir string, args ...string) (txnGitOutput, error) {
	if dir == "" {
		return txnGitOutput{}, errors.New("git working directory must not be empty")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, TxnGitTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"LANG=C",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	out := txnGitOutput{stdout: stdout.String(), stderr: stderr.String()}
	if cmdCtx.Err() != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(args, " "), cmdCtx.Err())
	}
	if err != nil {
		return out, &txnGitError{args: args, out: out, err: err}
	}
	return out, nil
}

// detectTxnGitRepo reports whether dir is inside a git work tree. A plain
// "not a git repository" is reported as (false, nil); anything else is a
// catastrophic error.
func detectTxnGitRepo(ctx context.Context, dir string) (bool, error) {
	out, err := runTxnGit(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err == nil {
		return strings.TrimSpace(out.stdout) == "true", nil
	}
	var gitErr *txnGitError
	// Safe only because runTxnGit pins LC_ALL=C: git never translates its
	// own fatal messages under it.
	if errors.As(err, &gitErr) && strings.Contains(strings.ToLower(gitErr.Error()), "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("txn: %s: %w", dir, err)
}

// txnBranch returns the checked-out branch name; "" for detached HEAD or
// failure (cosmetic in the status contract).
func txnBranch(ctx context.Context, dir string) string {
	if out, err := runTxnGit(ctx, dir, "branch", "--show-current"); err == nil {
		if name := strings.TrimSpace(out.stdout); name != "" {
			return name
		}
	}
	if out, err := runTxnGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		return strings.TrimSpace(out.stdout)
	}
	return ""
}

// txnHeadSha returns the full HEAD sha, or "" when HEAD does not resolve
// (unborn branch) or fails.
func txnHeadSha(ctx context.Context, dir string) string {
	out, err := runTxnGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.stdout)
}

// txnRepoRoot returns the absolute work-tree root of the repo containing
// dir, or "" when it cannot be determined.
func txnRepoRoot(ctx context.Context, dir string) string {
	out, err := runTxnGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(out.stdout)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

// treeChange is one parsed porcelain entry.
type treeChange struct {
	path      string // repo-root relative, forward slashes
	untracked bool
	deleted   bool
}

// collectTreeChanges runs `git status --porcelain -z -uall
// --untracked-files=all` once and parses every entry. Paths are
// repo-root-relative; a subdirectory invocation still reports paths
// relative to the repo ROOT, so the caller must join them onto the root —
// not onto the original dir.
//
// This is a deliberate re-implementation (~30 lines) of the parser shape in
// pkg/git/sync.go rather than a reach into that package: pkg/git is internal
// to the agent surface, and ETH-2 needs the deleted/untracked split the
// sync report does not carry.
func collectTreeChanges(ctx context.Context, dir string) ([]treeChange, error) {
	out, err := runTxnGit(ctx, dir, "status", "--porcelain", "-z", "-uall", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("txn: git status failed: %w", err)
	}
	return parseTxnPorcelain(out.stdout), nil
}

// parseTxnPorcelain parses NUL-terminated `git status --porcelain -z -uall`
// output into ordered entries. Rename/copy entries occupy two
// NUL-terminated fields (new path, then original path); the original is
// consumed so it is not mistaken for an entry of its own — a rename is
// reported as its NEW path being dirty, never as the old path being deleted.
func parseTxnPorcelain(zOutput string) []treeChange {
	changes := []treeChange{}
	if zOutput == "" {
		return changes
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
			changes = append(changes, treeChange{path: path, untracked: true})
			continue
		}
		deleted := x == 'D' || y == 'D'
		changes = append(changes, treeChange{path: path, deleted: deleted})
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // skip the original-path field of a rename/copy pair
		}
	}
	return changes
}

// validateRelPath applies the contract's repo-relative path rules and
// returns the skip reason for an unusable path ("" when the path is safe to
// join onto the workdir).
//
// Rules: forward-slash separated, no drive letters or leading "/", no ".."
// segment, no ".git" segment anywhere, no NUL, no empty/"/" segments.
func validateRelPath(path string) string {
	switch {
	case path == "" || strings.TrimSpace(path) == "":
		return SkipReasonEmptyPath
	case strings.ContainsRune(path, '\x00'):
		return SkipReasonNulInPath
	case isAbsAnyOS(path):
		return SkipReasonAbsolutePath
	}
	for _, seg := range strings.Split(path, "/") {
		switch seg {
		case "..":
			return SkipReasonPathTraversal
		case ".git":
			return SkipReasonGitPath
		case "", ".":
			return SkipReasonInvalidPath
		}
	}
	return ""
}

// isAbsAnyOS rejects Unix absolute paths and Windows drive/UNC forms on
// every platform, so a manifest authored on one OS cannot smuggle an
// absolute path past a container running another.
func isAbsAnyOS(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	if os.PathSeparator != '\\' {
		// On non-Windows the backslash is an ordinary filename character,
		// but a leading "\\" or "X:\\" is unambiguously a Windows path and
		// never a legitimate repo-relative name.
		if strings.HasPrefix(path, `\\`) {
			return true
		}
		if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') &&
			((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) {
			return true
		}
		return false
	}
	// Windows: filepath.IsAbs already covered drive/UNC; a lone embedded
	// backslash is a separator there, so treat any "\\" as one.
	return strings.ContainsRune(path, '\\')
}

// resolveWorkdir normalizes the working directory for git invocations. It
// is never empty: an empty dir would send commands against the process CWD
// (the hazard pkg/git/safety.go guards against).
func resolveWorkdir(dir string) (string, error) {
	if strings.TrimSpace(dir) != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("txn: no working directory given and failed to resolve the process working directory: %w", err)
	}
	return cwd, nil
}

// secureJoin returns the absolute path of rel under root, refusing any
// resolution that leaves root.
//
// Symlinks are the escape hatch: a component directory (or the final entry
// itself, when it already exists) may be a link pointing outside root. The
// nearest EXISTING ancestor is therefore resolved with filepath.EvalSymlinks
// and re-checked for containment, and the missing remainder re-joined below
// it — a missing final file is fine, its parent is what must be real.
func secureJoin(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("txn: resolve workdir %s: %w", root, err)
	}
	if evaled, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = evaled
	}

	full := filepath.Join(rootAbs, filepath.FromSlash(rel))

	anchor, tail := full, ""
	for {
		if _, err := os.Lstat(anchor); err == nil {
			break
		}
		tail = filepath.Join(filepath.Base(anchor), tail)
		parent := filepath.Dir(anchor)
		if parent == anchor {
			return "", fmt.Errorf("txn: no existing ancestor under %s for %q", rootAbs, rel)
		}
		anchor = parent
	}

	resolved, err := filepath.EvalSymlinks(anchor)
	if err != nil {
		return "", fmt.Errorf("txn: resolve %q: %w", rel, err)
	}
	candidate := filepath.Join(resolved, tail)
	if !withinRoot(rootAbs, candidate) {
		return "", errEscapesRoot
	}

	// The candidate may itself exist as a symlink aimed outside root; a
	// write through it would land there, so evaluate and re-check.
	if _, err := os.Lstat(candidate); err == nil {
		evaled, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			if !withinRoot(rootAbs, evaled) {
				return "", errEscapesRoot
			}
			candidate = evaled
		}
	}
	return candidate, nil
}

// withinRoot reports whether path is root or lives underneath it.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// resolveRunWorkdir confines a run request's "workdir" field. It is
// repo-relative like every other path in the contract: an absolute value or
// any ".." escape is refused outright (a 400 at the HTTP layer) rather than
// skipped, because there is no per-entry skip slot for it.
func resolveRunWorkdir(root, workdir string) (string, error) {
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" || trimmed == "." {
		return root, nil
	}
	if reason := validateRelPath(trimmed); reason != "" {
		return "", fmt.Errorf("txn: invalid workdir %q: %s", workdir, reason)
	}
	return secureJoin(root, trimmed)
}
