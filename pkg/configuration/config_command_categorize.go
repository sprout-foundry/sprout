// Package configuration: command categorization and force-flag detection.
// (split from config_risk_subagent.go)
package configuration

import (
	"strings"
)

// containsForceFlag checks if a command string contains -f or --force flags.
// --force-with-lease is explicitly excluded as it is a safer alternative
// that verifies remote state before overwriting.
// For -f standalone, only treats it as force for commands that commonly use -f as a force flag:
// git, rm, mv, cp, and docker. This avoids false positives on commands like grep -f or tail -f.
func containsForceFlag(cmdLower string) bool {
	// Check for --force as an exact token, but NOT --force-with-lease
	for _, segment := range strings.Fields(cmdLower) {
		if segment == "--force" {
			return true
		}
	}

	// Get the first word (command name) to check if -f should be treated as force
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	firstCmd := fields[0]

	// Check for -f as a standalone flag (not part of a word)
	// Only treat -f as force for commands that commonly use it as a force flag
	for idx, segment := range fields {
		if segment == "-f" {
			// Only -f for force-capable commands
			switch firstCmd {
			case "git":
				// For git, -f must appear AFTER the subcommand (not at position 1).
				// Position 1 is between git and the subcommand — that's a malformed
				// global flag position, not a force flag.
				// Exception: if -f is the last token (e.g. "git -f" with no subcommand),
				// treat it as force — bare git -f is unusual but should be flagged.
				if idx > 1 {
					return true
				}
				if idx == 1 && idx == len(fields)-1 {
					return true // "git -f" with nothing after — bare force flag
				}
				// idx == 1 and there are more tokens → malformed global flag, skip
			case "rm", "mv", "cp", "docker":
				return true
			}
		}
		// Handle combined short flags like -af, -rf (these are dangerous)
		// Only treat combined flags with 'f' as force for force-capable commands
		if len(segment) > 2 && segment[0] == '-' && segment[1] != '-' && strings.Contains(segment, "f") {
			switch firstCmd {
			case "git":
				// Same rule: for git, combined flags with 'f' at position 1 are
				// skipped if there's a subcommand after them.
				if idx == 1 && idx < len(fields)-1 {
					continue
				}
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			case "rm", "mv", "cp", "docker":
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			}
		}
	}
	return false
}

// categorizeCommand maps a command string to a risk-category identifier.
func categorizeCommand(cmdLower string) string {
	if strings.HasPrefix(cmdLower, "git ") {
		return categorizeGitCommand(cmdLower)
	}
	if strings.HasPrefix(cmdLower, "rm ") {
		return "rm_command"
	}
	if strings.HasPrefix(cmdLower, "docker ") {
		return "docker"
	}
	// Read-only file operations. These commands cannot mutate state:
	// they only read files / metadata / environment / hardware info.
	// Commands commonly invoked WITHOUT arguments (pwd, date, whoami,
	// ...) are matched via isReadOnlyCmd which accepts both the bare
	// command and "<cmd> <args>" forms. The remaining ones are matched
	// by prefix only (they always take an argument in practice).
	if isReadOnlyCmd(cmdLower,
		// Bare-or-arged: frequently invoked with no arguments.
		"pwd", "date", "whoami", "id", "groups", "tty", "arch",
		"nproc", "uptime", "free", "true", "false", "env", "printenv",
		// Always-arged in practice, but matched uniformly for simplicity.
		"cat", "head", "ls", "find", "which", "file",
		"grep", "rg", "wc", "tree", "du", "df", "stat", "uname",
		"basename", "dirname", "realpath", "test",
		"type", "command", "hash", "locate", "lscpu", "lsblk", "lsmod",
		"lspci", "lsusb", "getconf",
	) {
		return "read_file"
	}
	// Write operations
	if strings.HasPrefix(cmdLower, "write_file") || strings.HasPrefix(cmdLower, "edit_file") {
		return "write_file"
	}
	// Build / test / lint tool invocations. These execute a project's
	// build system, test runner, or linter — read-mostly operations that
	// are the single most common source of shell_command prompts during
	// development. Only the safe subcommands are recognized; state-mutating
	// forms (install, apply, delete, publish, push, prune, system prune,
	// exec, run --rm that mutates) fall through to shell_command → Medium.
	if isBuildTestCmd(cmdLower) {
		return "build_test"
	}
	return "shell_command"
}

// isReadOnlyCmd reports whether cmdLower is an invocation of one of the
// given read-only command names — either the bare command (e.g. "pwd")
// or the command followed by arguments ("pwd\n", "pwd ", "pwd\n…"). The
// trailing-space prefix form matches any argument-bearing invocation
// while the exact-equality form covers the common no-argument case.
func isReadOnlyCmd(cmdLower string, names ...string) bool {
	for _, n := range names {
		if cmdLower == n || strings.HasPrefix(cmdLower, n+" ") {
			return true
		}
	}
	return false
}

// buildTestSafeSubcommands lists the subcommands of npm/yarn/pnpm that are
// treated as build/test/lint (auto-approved) rather than state-mutating.
// Anything not listed here (install, publish, uninstall, add, remove,
// create, etc.) falls through to shell_command → Medium.
var buildTestSafeSubcommands = map[string]bool{
	"test": true, "tests": true, "e2e": true,
	"build": true, "build-all": true,
	"lint": true, "format": true, "fmt": true,
	"check": true, "vet": true, "typecheck": true, "type-check": true,
	"storybook": true, "coverage": true,
	"run": true, "exec": true, // `npm run X` / `npm exec X` — user-defined scripts
}

// isBuildTestCmd reports whether cmdLower is a recognized build / test / lint
// tool invocation. It matches:
//
//   - Bare tools that are inherently read/exec-only on local state:
//     go, make, cargo, mvn, gradle, dotnet.
//   - Script runners (node, python3/python, ruby, perl) — running a script
//     is the development primitive; the classifier cannot inspect what the
//     script does, but treating it as Medium on every invocation produces
//     a prompt storm with no real safety benefit (Critical ops inside the
//     script like `rm -rf /` are caught by the static classifier when the
//     shell handler expands them, not here).
//   - npm/yarn/pnpm with a safe subcommand (test, build, lint, run, …).
//     State-mutating subcommands (install, publish, add, remove) are NOT
//     matched and fall through to shell_command → Medium.
//   - Safe kubectl (get, describe, logs, top, explain) and terraform
//     (plan, validate, fmt) — read-only inspection subcommands.
//
// The common false-positive vectors are excluded deliberately:
//   - `go install` → "go" matches but install mutates GOPATH/pkg cache;
//     however go's safe surface (build, test, vet, run) is large enough
//     that we accept go as a unit and rely on the shell handler's
//     pipeline-expansion check for genuinely destructive `go run` scripts.
//   - `docker run/exec` mutates containers but not the host; treated as
//     build_test (docker compose up/down is the common dev primitive).
func isBuildTestCmd(cmdLower string) bool {
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]
	switch base {
	// Bare tools: every invocation is a build/test/lint primitive.
	case "make", "cargo", "mvn", "gradle", "dotnet", "msbuild":
		return true
	case "go":
		// `go install`/`go get` mutate module cache — route to shell_command.
		// Everything else (build, test, vet, run, generate, mod tidy, etc.)
		// is a development primitive.
		if len(fields) >= 2 && (fields[1] == "install" || fields[1] == "get") {
			return false
		}
		return true
	// Script runners: executing a script file is the dev primitive.
	case "node", "python", "python3", "ruby", "perl", "deno", "bun":
		return true
	// npm/yarn/pnpm: only safe subcommands.
	case "npm", "yarn", "pnpm", "npx":
		// Bare `npm` or `yarn` with no subcommand is not a recognized op.
		if len(fields) < 2 {
			return false
		}
		return buildTestSafeSubcommands[fields[1]]
	// kubectl: only read-only inspection subcommands.
	case "kubectl", "oc", "k":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "get", "describe", "logs", "log", "top", "explain",
			"version", "config", "cluster-info", "api-resources", "api-versions":
			return true
		}
		return false
	// terraform: plan/validate/fmt/version are read-only.
	case "terraform", "tofu", "tflint":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "plan", "validate", "fmt", "version", "show", "graph", "output":
			return true
		}
		return false
	// docker: compose up/down/ps/logs (dev workflow) and ps/images/logs (read).
	// docker run/exec/prune/system fall through to shell_command → Medium.
	case "docker", "podman":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "compose", "ps", "images", "logs", "log", "version", "info",
			"stats", "top", "port", "inspect":
			return true
		}
		return false
	}
	return false
}
