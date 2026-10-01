package tools

import (
	"strings"
)

// shell_patterns_privilege.go — the privilege-escalation and escape
// detectors (sudo, privileged package installs, destructive find,
// interpreter one-liners), split out of the original shell_patterns.go
// monolith. Pure move.

// isSudoCommand reports whether cmd is a sudo-prefixed command.
func isSudoCommand(cmd string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	return strings.HasPrefix(cmdLower, "sudo ")
}

func isPrivilegedPackageInstall(cmd string) bool {
	normalized := strings.TrimSpace(strings.ToLower(cmd))
	installPrefixes := []string{
		"sudo apt-get install",
		"sudo apt install",
		"sudo yum install",
		"sudo dnf install",
		"sudo brew install",
		"sudo snap install",
		"sudo flatpak install",
		"sudo apk add",
	}
	for _, prefix := range installPrefixes {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return true
		}
	}
	return false
}

func containsPrivilegedPackageInstall(cmd string) bool {
	parts := strings.FieldsFunc(cmd, func(r rune) bool {
		return r == ';' || r == '|'
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		subparts := strings.Split(part, "&&")
		for _, sub := range subparts {
			for _, candidate := range strings.Split(sub, "||") {
				if isPrivilegedPackageInstall(candidate) {
					return true
				}
			}
		}
	}
	return false
}

// isDestructiveFind detects find commands with destructive action flags.
// Returns true when the find command uses -delete or -exec/-execdir with
// rm, chmod, or chown — all of which can destroy data in bulk.
//
// The cmd parameter is already lowercased by the caller. We use simple
// substring checks since find action flags always start with '-' and
// the destructive actions are short, well-known tokens.
func isDestructiveFind(cmd string) bool {
	if !strings.HasPrefix(cmd, "find ") && !strings.HasPrefix(cmd, "find/") {
		return false
	}
	// Handle both "find " prefix and bare "find" with no space (edge case)
	if cmd == "find" {
		return false
	}

	// -delete removes matched files directly
	if strings.Contains(cmd, " -delete") {
		return true
	}
	// -exec / -execdir with destructive inner commands. Match the destructive
	// token only AFTER the -exec position so an unrelated token earlier in
	// the find expression (e.g. `-name "foo rm bar"`) doesn't trigger a false
	// positive. `-execdir` contains `-exec` as a substring, so one Index call
	// covers both forms.
	if idx := strings.Index(cmd, "-exec"); idx >= 0 {
		tail := cmd[idx:]
		return strings.Contains(tail, " rm") ||
			strings.Contains(tail, " chmod") ||
			strings.Contains(tail, " chown")
	}
	return false
}

// isInterpreterCommandEscape detects when a shell or language interpreter
// is invoked with an inline-code flag (-c, -e, -r, --eval). The body of
// inline code is opaque to a static classifier — we can't tell if it's
// benign ("python -c 'print(1)'") or destructive ("bash -c 'rm -rf /'").
// The correct tier is CAUTION (prompt the user), not SAFE.
//
// This does NOT match running a script file (e.g., "bash script.sh" or
// "python script.py") — those remain safe since the script name is a
// filename, not inline code.
//
// The cmd parameter is already lowercased by the caller.
func isInterpreterCommandEscape(cmd string) bool {
	// Shell interpreters with -c flag
	interpreterFlags := []string{
		"bash -c ", "bash -c\t",
		"sh -c ", "sh -c\t",
		"zsh -c ", "zsh -c\t",
		"fish -c ", "fish -c\t",
		"dash -c ", "dash -c\t",
		// Python
		"python -c ", "python -c\t",
		"python3 -c ", "python3 -c\t",
		// Node.js
		"node -e ", "node -e\t",
		"node --eval ", "node --eval\t",
		// Perl
		"perl -e ", "perl -e\t",
		"perl -E ", "perl -E\t",
		// Ruby
		"ruby -e ", "ruby -e\t",
		"ruby -E ", "ruby -E\t",
		// PHP
		"php -r ", "php -r\t",
	}
	for _, pattern := range interpreterFlags {
		if strings.HasPrefix(cmd, pattern) || cmd == strings.TrimSpace(pattern) {
			return true
		}
	}
	return false
}
