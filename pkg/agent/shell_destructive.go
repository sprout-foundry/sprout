// Destructive shell command classifier.
//
// Peer to shellLooksReadOnly. Identifies shell commands that are likely
// to clobber the user's active changes — either by reverting working-tree
// edits (`git checkout .`, `git reset --hard`, `git restore .`) or by
// deleting untracked work (`git clean -fd`, `git stash drop`, etc.).
//
// When the change tracker sees a destructive command, it pivots to a
// safer (slower) mode:
//
//   - The adaptive autoSkipDirs set is IGNORED for the walk (and the
//     walk doesn't add to it during a destructive run). A directory
//     that was learned as "fat" during a build might contain edits the
//     user wants back after a `git checkout .` — we'd rather pay the
//     walk cost than silently drop the recovery payload.
//   - The bulk-rollup branch in RecordShellMutations is BYPASSED so
//     every mutation lands as a per-file entry with full
//     OriginalCode for recovery. A 300-file `git checkout .` produces
//     300 recoverable rows, not one opaque "src/ — 300 files" row.
//   - Truncation (50k file / 500ms / 32 MiB caps) gets promoted from
//     a log line to a user-visible manifest entry so partial coverage
//     during a destructive op is impossible to miss.
//
// Bias: CONSERVATIVE in the opposite direction from shellLooksReadOnly.
// False positive ("said destructive when it wasn't") means we run a
// fuller walk and emit per-file for a normal command — cheap. False
// negative ("missed a destructive op") means we might silently drop a
// recoverable change — expensive. So unrecognised flags or subcommands
// on a known-destructive program err toward "destructive".
package agent

import (
	"path/filepath"
	"strings"
)

// shellIsDestructive returns true when `command` is the kind of shell
// invocation that can wipe active changes (reverting the working tree,
// dropping a stash, cleaning untracked files, etc.). Used by the change
// tracker to switch into the safer per-file-with-no-auto-skip mode.
//
// Always-false for commands shellLooksReadOnly would accept — read-only
// invocations can't clobber anything by definition.
func shellIsDestructive(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}

	// If the line is a chain (`git pull && make`) we treat the WHOLE
	// thing as destructive when ANY segment is destructive. The walk
	// runs once around the entire shell_command invocation, so it has
	// to cover the worst case.
	if hasShellChainOperator(cmd) {
		for _, seg := range splitShellSegments(cmd) {
			if shellIsDestructive(seg) {
				return true
			}
		}
		return false
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	program := filepath.Base(fields[0])

	switch program {
	case "git":
		return isDestructiveGitInvocation(fields[1:])
	}
	return false
}

// hasShellChainOperator detects `&&`, `||`, `;`, and `|` so we can
// recurse into each segment. Distinct from
// hasShellMutationOperators in shell_readonly.go (which also flags
// redirects and command-substitution as "not provably read-only").
// Here we only care about top-level command boundaries.
func hasShellChainOperator(cmd string) bool {
	for _, op := range []string{"&&", "||", ";", "|"} {
		if strings.Contains(cmd, op) {
			return true
		}
	}
	return false
}

// splitShellSegments breaks `cmd` at top-level `&&`, `||`, `;`, `|`
// boundaries. Doesn't understand quoting — quoted operators would split
// incorrectly, but the cost is just over-classifying as destructive,
// which is the safe direction.
func splitShellSegments(cmd string) []string {
	// Replace multi-char ops with a single delimiter, then split.
	replaced := cmd
	for _, op := range []string{"&&", "||"} {
		replaced = strings.ReplaceAll(replaced, op, "\x00")
	}
	for _, op := range []string{";", "|"} {
		replaced = strings.ReplaceAll(replaced, op, "\x00")
	}
	parts := strings.Split(replaced, "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isDestructiveGitInvocation classifies `git <args...>` after the
// `git` token has been stripped. Skips leading global flags
// (`-c key=val`, `-C dir`, `--git-dir=`, `--work-tree=`) so they don't
// throw off the subcommand match.
func isDestructiveGitInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	i := 0
	for i < len(args) {
		switch {
		case args[i] == "-c" || args[i] == "-C":
			i += 2
		case strings.HasPrefix(args[i], "--git-dir=") || strings.HasPrefix(args[i], "--work-tree="):
			i++
		case strings.HasPrefix(args[i], "-"):
			i++
		default:
			goto subcmd
		}
		if i >= len(args) {
			return false
		}
	}
subcmd:
	if i >= len(args) {
		return false
	}
	subcmd := args[i]
	rest := args[i+1:]

	switch subcmd {
	case "checkout":
		// `git checkout` covers a wide range: switching branches (could
		// modify working tree), reverting paths (`git checkout .`),
		// `git checkout -- file`. All of these can revert active edits.
		// `git checkout -b new-branch` doesn't modify files, but it's
		// rare enough that treating it as destructive is acceptable
		// noise.
		return true

	case "restore":
		// `git restore .`, `git restore --staged .`, `git restore --source=…`
		// — all variants revert the working tree or index.
		return true

	case "reset":
		// `git reset` (no flags) only moves HEAD and updates the index;
		// the working tree is preserved. `--hard`, `--merge`, `--keep`
		// touch the working tree. Conservative: any `reset` with a flag
		// we don't explicitly recognize as safe counts as destructive.
		for _, a := range rest {
			switch a {
			case "--hard", "--merge", "--keep":
				return true
			}
		}
		return false

	case "stash":
		// All `git stash` operations that aren't read-only (list/show)
		// are destructive to the working tree:
		// - bare `git stash` / `stash push`: saves and REVERTS the working tree to HEAD
		// - `stash pop`/`apply`: restores via 3-way merge that can silently revert files
		// - `stash drop`/`clear`: discards saved state
		// `stash list`/`show` are read-only (handled by shellLooksReadOnly),
		// but if we reach here they still count as destructive for cache purposes
		// (conservative: err toward re-priming).
		if len(rest) == 0 {
			return true // bare `git stash` == `stash push` — reverts working tree
		}
		switch rest[0] {
		case "list", "show":
			return false
		}
		return true

	case "clean":
		// `git clean` is always destructive when paired with -f/--force
		// (it refuses to run otherwise). Any -f/--force/-x/-X/-d invocation
		// is removing untracked files — exactly what we want recoverable.
		for _, a := range rest {
			if a == "-f" || a == "--force" ||
				a == "-fd" || a == "-df" ||
				a == "-x" || a == "-X" || a == "-d" ||
				strings.HasPrefix(a, "-f") {
				return true
			}
		}
		return false

	case "rebase":
		// `rebase --abort` and `rebase --continue` modify the working
		// tree; plain `rebase <onto>` does too. `rebase --skip` advances
		// past a commit. All can leave files in unexpected states.
		// `rebase -i` opens an editor but ultimately rewrites the tree.
		return true

	case "merge":
		// `merge --abort` reverts an in-progress merge (destructive).
		// `merge <ref>` can fast-forward or three-way merge — both
		// modify files. `--no-commit` still modifies the working tree.
		return true

	case "pull":
		// `pull` is fetch + merge (or rebase). Either branch can
		// rewrite the working tree.
		return true

	case "revert":
		// Creates a new commit that undoes prior changes — modifies
		// the working tree as part of the revert.
		return true

	case "cherry-pick":
		// Applies a commit's diff to the working tree.
		return true

	case "am":
		// `git am` applies a mailbox of patches.
		return true

	case "apply":
		// `git apply patch.diff` writes to the working tree. Read-only
		// modes (`--check`, `--stat`, `--summary`) don't modify, but the
		// default does.
		for _, a := range rest {
			if a == "--check" || a == "--stat" || a == "--summary" || a == "--numstat" {
				return false
			}
		}
		return true

	case "rm":
		// `git rm <path>` deletes from working tree AND index.
		return true

	case "mv":
		// `git mv` is rename + index update — recoverable info loss if
		// it stomps an existing file.
		return true

	case "switch":
		// `git switch <branch>` modifies the working tree the same way
		// `git checkout <branch>` does.
		return true
	}
	return false
}
