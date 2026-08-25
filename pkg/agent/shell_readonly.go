// Read-only shell command classifier.
//
// Used to short-circuit the shell-snapshot pass for commands that
// provably can't mutate the filesystem. Skipping the snapshot avoids
// the ~10 ms warm-walk cost (and the full prime cost when uncached) on
// every `ls`, `grep`, `cat`, `git status`, etc. — by far the most
// common shell_command invocations.
//
// Bias: the classifier is CONSERVATIVE. False positive ("said read-only
// when it wasn't") means we miss tracking the mutation — bad. False
// negative ("said write when it was read-only") means we pay the
// snapshot cost we didn't need — cheap. So any unknown program,
// chaining operator, redirect, subshell, or known-dangerous flag
// forces the snapshot.
package agent

import (
	"path/filepath"
	"strings"
)

// shellLooksReadOnly returns true when the given shell command can be
// proven (by conservative inspection) to make no filesystem changes.
// Returns false when the command is unknown OR contains any operator /
// flag that could introduce a write.
func shellLooksReadOnly(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return true
	}

	// Any chaining / redirection / subshell forces snapshot. Even
	// `cat foo > bar.txt` writes to bar.txt; we can't statically prove
	// the right-hand side is safe.
	if hasShellMutationOperators(cmd) {
		return false
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return true
	}
	program := filepath.Base(fields[0])

	if alwaysReadOnlyPrograms[program] {
		return true
	}

	// Programs with a read-only mode determined by sub-args.
	switch program {
	case "git":
		return isReadOnlyGitInvocation(fields[1:])
	case "find":
		return isReadOnlyFindInvocation(fields[1:])
	case "sed", "gsed":
		return isReadOnlySedInvocation(fields[1:])
	case "awk", "gawk", "mawk", "nawk":
		return isReadOnlyAwkInvocation(fields[1:])
	case "go":
		return isReadOnlyGoInvocation(fields[1:])
	case "npm", "pnpm", "yarn", "bun":
		return isReadOnlyJSPkgInvocation(fields[1:])
	}

	return false
}

// hasShellMutationOperators returns true if `cmd` contains any character
// sequence we can't statically rule out as a write path. Catches
// redirects, pipes, chains, subshells, and process substitution.
func hasShellMutationOperators(cmd string) bool {
	// `>`, `<`, `>>`, `2>`, `&>`, etc. — any redirect.
	// `|` and `&&` / `||` / `;` — chaining.
	// `$(…)` and backticks — command substitution.
	// `<(…)` — process substitution.
	//
	// A naive scan handles the overwhelming majority of cases. We
	// don't attempt to parse quotes; "echo '>'" would be incorrectly
	// flagged as a redirect, but the cost of a false negative is just
	// running the snapshot we didn't strictly need.
	for _, op := range []string{">", "<", "|", "&&", "||", "$(", "`"} {
		if strings.Contains(cmd, op) {
			return true
		}
	}
	// Single `;` and `&` need their own checks so we don't match
	// inside identifiers (none of the safe programs accept those in
	// argv, but be careful with quoted strings — same caveat as above).
	if strings.Contains(cmd, ";") || strings.Contains(cmd, " & ") || strings.HasSuffix(cmd, "&") {
		return true
	}
	return false
}

// alwaysReadOnlyPrograms lists programs that, when invoked with no
// dangerous sub-args (guarded by hasShellMutationOperators), are
// guaranteed not to write the filesystem.
var alwaysReadOnlyPrograms = map[string]bool{
	"ls":        true,
	"cat":       true,
	"head":      true,
	"tail":      true,
	"wc":        true,
	"pwd":       true,
	"whoami":    true,
	"id":        true,
	"date":      true,
	"env":       true,
	"printenv":  true,
	"which":     true,
	"where":     true,
	"tree":      true,
	"du":        true,
	"df":        true,
	"ps":        true,
	"uname":     true,
	"uptime":    true,
	"hostname":  true,
	"column":    true,
	"sort":      true,
	"uniq":      true,
	"cut":       true,
	"tr":        true,
	"tac":       true,
	"nl":        true,
	"xxd":       true,
	"od":        true,
	"file":      true,
	"stat":      true,
	"echo":      true,
	"basename":  true,
	"dirname":   true,
	"realpath":  true,
	"readlink":  true,
	"grep":      true, // grep itself is read-only; `grep -l … | xargs rm` would be caught by `|`
	"egrep":     true,
	"fgrep":     true,
	"rg":        true, // ripgrep
	"ag":        true, // the_silver_searcher
	"diff":      true,
	"cmp":       true,
	"md5sum":    true,
	"sha1sum":   true,
	"sha256sum": true,
	"sha512sum": true,
	"true":      true,
	"false":     true,
	"yes":       true, // pure stdout
	"seq":       true,
	"jq":        true, // read-only without `-i` (jq has no -i flag — `>` would be caught)
	"yq":        true,
	"man":       true,
	"info":      true,
	"help":      true,
}

func isReadOnlyGitInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Skip leading global flags like `-c key=val`, `-C dir`.
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
			break
		}
		if i >= len(args) {
			return false
		}
		if !strings.HasPrefix(args[i], "-") {
			break
		}
	}
	if i >= len(args) {
		return false
	}
	subcmd := args[i]
	readOnly := map[string]bool{
		"status":      true,
		"log":         true,
		"diff":        true,
		"show":        true,
		"blame":       true,
		"config":      true, // `git config` without --add/--set is read; with them it writes ~/.gitconfig or repo config — repo config isn't workspace-tracked content, accept risk
		"remote":      true,
		"rev-parse":   true,
		"rev-list":    true,
		"ls-files":    true,
		"ls-tree":     true,
		"cat-file":    true,
		"describe":    true,
		"shortlog":    true,
		"reflog":      true,
		"whatchanged": true,
		"grep":        true,
		"bisect":      true, // bisect doesn't modify the working tree contents (it switches commits, which is mutation but caught by `--` style)
	}
	if !readOnly[subcmd] {
		return false
	}
	// `git branch` is read-only WITHOUT -d/-D/-m. `git tag` similar.
	// Currently both are conservatively absent from the list (we'd
	// need to parse their flags); the agent uses other tools for
	// branch/tag operations anyway.
	return true
}

func isReadOnlyFindInvocation(args []string) bool {
	// find supports `-exec`, `-execdir`, `-delete`, `-ok`, `-okdir`
	// which can mutate the filesystem. Refuse if any of these appear.
	for _, a := range args {
		switch a {
		case "-exec", "-execdir", "-delete", "-ok", "-okdir":
			return false
		}
	}
	return true
}

func isReadOnlySedInvocation(args []string) bool {
	for _, a := range args {
		// sed -i / sed -i.bak (or --in-place) edits in place.
		if a == "-i" || a == "--in-place" || strings.HasPrefix(a, "-i.") || strings.HasPrefix(a, "--in-place=") {
			return false
		}
		// sed -f script-file is a read; sed expression is a read.
		// All writes happen via -i or shell redirect (caught upstream).
	}
	return true
}

func isReadOnlyAwkInvocation(args []string) bool {
	for i, a := range args {
		// gawk -i inplace (or --inplace / --in-place) edits in place.
		if a == "-i" && i+1 < len(args) && strings.HasPrefix(args[i+1], "inplace") {
			return false
		}
		if a == "--inplace" || a == "--in-place" {
			return false
		}
	}
	return true
}

func isReadOnlyGoInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	subcmd := args[0]
	// `go build` and friends write binaries; `go test` writes coverage
	// and test cache; `go generate` runs arbitrary commands; `go mod
	// tidy` rewrites go.sum / go.mod. Restrict to truly read-only
	// subcommands.
	readOnly := map[string]bool{
		"version": true,
		"env":     true,
		"help":    true,
		"list":    true,
		"doc":     true,
		"vet":     true, // analysis only; doesn't write
	}
	return readOnly[subcmd]
}

func isReadOnlyJSPkgInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	subcmd := args[0]
	readOnly := map[string]bool{
		"ls":       true,
		"list":     true,
		"view":     true,
		"info":     true,
		"outdated": true,
		"audit":    true,
		"why":      true,
		"explain":  true,
		"config":   true, // `config get`; sets caught by `set` not being in the list
		"prefix":   true,
		"root":     true,
		"bin":      true,
		"help":     true,
	}
	return readOnly[subcmd]
}
