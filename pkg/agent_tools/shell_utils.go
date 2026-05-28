package tools

import (
	"path"
	"strings"
)

// stripQuotedSections replaces the content of quoted strings (single and double
// quotes) with spaces, preserving string length. This is used before pattern
// matching to avoid false positives from | or other shell metacharacters that
// appear inside quoted argument values (e.g., grep regex alternation like
// "rgba|gradient|shadow|image").
func stripQuotedSections(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if inQuote {
			b.WriteByte(' ')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// maxRisk returns the maximum risk level from a slice
func maxRisk(risks []SecurityRisk) SecurityRisk {
	max := SecuritySafe
	for _, r := range risks {
		if r > max {
			max = r
		}
	}
	return max
}

func extractCommandSubstitutions(cmd string) []string {
	var subs []string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '$' || i+1 >= len(cmd) || cmd[i+1] != '(' {
			continue
		}
		start := i + 2
		depth := 1
		j := start
		for ; j < len(cmd); j++ {
			switch cmd[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					sub := strings.TrimSpace(cmd[start:j])
					if sub != "" {
						subs = append(subs, sub)
					}
					i = j
					break
				}
			}
			if depth == 0 {
				break
			}
		}
	}
	return subs
}

// containsRedirection returns true if the command contains output redirection
// operators (>, >>) that could write to arbitrary paths
func containsRedirection(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		r := cmd[i]
		if r == '\'' {
			for i++; i < len(cmd) && cmd[i] != '\''; i++ {
			}
			continue
		}
		if r == '"' {
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
			}
			continue
		}
		// File descriptor duplication (2>&1, 1>&2, etc.) is not file output redirection
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '&' {
			continue
		}
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '>' {
			return true
		}
		if r == '>' && (i+1 >= len(cmd) || cmd[i+1] != '=') {
			if i == 0 || cmd[i-1] != '>' {
				return true
			}
		}
	}
	return false
}

// extractRedirectionTarget extracts the path from the first output redirection
// in the command string. Handles both > and >> operators. Returns the cleaned
// path and true if found, or empty string and false if not found.
// Correctly distinguishes >> from > by checking >> first at each position.
func extractRedirectionTarget(lower string) (string, bool) {
	for i := 0; i < len(lower); i++ {
		if lower[i] != '>' {
			continue
		}
		// Check for >> first (append redirect)
		redirectLen := 1
		if i+1 < len(lower) && lower[i+1] == '>' {
			redirectLen = 2
		}
		// Skip file descriptor duplication (e.g., 2>&1)
		if i+redirectLen < len(lower) && lower[i+redirectLen] == '&' {
			continue
		}
		pathStart := i + redirectLen
		// Skip whitespace after redirect operator
		for pathStart < len(lower) && lower[pathStart] == ' ' {
			pathStart++
		}
		if pathStart >= len(lower) {
			continue
		}
		// Extract path until whitespace, semicolon, pipe, or &
		pathEnd := pathStart
		for pathEnd < len(lower) {
			c := lower[pathEnd]
			if c == ' ' || c == ';' || c == '|' || c == '&' {
				break
			}
			pathEnd++
		}
		if pathEnd > pathStart {
			pathStr := lower[pathStart:pathEnd]
			cleaned := path.Clean(pathStr)
			return cleaned, true
		}
		// Only process the first redirection
		break
	}
	return "", false
}

// isBenignRedirection returns true if output redirection targets known harmless sinks.
// For /tmp paths, validates that the cleaned path stays within /tmp to prevent
// path traversal attacks like > /tmp/../etc/passwd.
func isBenignRedirection(cmd string) bool {
	lower := strings.ToLower(cmd)

	// Extract first redirection target and validate
	cleaned, found := extractRedirectionTarget(lower)
	if found {
		if strings.HasPrefix(cleaned, "/tmp/") || cleaned == "/tmp" {
			return true
		}
	}

	// /dev/null, /dev/stdout, /dev/stderr are always safe
	return strings.Contains(lower, "> /dev/null") || strings.Contains(lower, ">> /dev/null") ||
		strings.Contains(lower, ">/dev/null") ||
		strings.Contains(lower, "> /dev/stdout") || strings.Contains(lower, ">> /dev/stdout") ||
		strings.Contains(lower, "> /dev/stderr") || strings.Contains(lower, ">> /dev/stderr")
}

// hasRedirectionTraversalToSystemDir checks if a command with output redirection
// uses path traversal to target a system directory. For example, > /tmp/../etc/passwd
// appears to target /tmp but actually resolves to /etc/passwd.
func hasRedirectionTraversalToSystemDir(cmd string) bool {
	lower := strings.ToLower(cmd)
	systemPrefixes := []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/opt/", "/root/", "/boot/"}

	cleaned, found := extractRedirectionTarget(lower)
	if !found {
		return false
	}
	for _, sys := range systemPrefixes {
		if strings.HasPrefix(cleaned, sys) {
			return true
		}
	}
	return false
}

// getShellCommandReasoning returns a human-readable reasoning string
func getShellCommandReasoning(cmd string, risk SecurityRisk) string {
	switch risk {
	case SecuritySafe:
		return "Read-only or safe workspace operation"
	case SecurityCaution:
		if containsPrivilegedPackageInstall(cmd) {
			return "Privileged package installation requested - review before continuing"
		}
		return "Potentially risky operation - review carefully"
	case SecurityDangerous:
		return "Dangerous operation detected - may cause data loss or system damage"
	default:
		return "Unknown operation type"
	}
}

// getShellCommandRiskType returns a risk category string for user-facing messages
func getShellCommandRiskType(cmd string, risk SecurityRisk, isCritical bool) string {
	if risk != SecurityDangerous {
		return ""
	}

	cmdLower := strings.ToLower(cmd)

	// rm -rf . or ~ or / or * or .git — mass deletion (check this first for specificity)
	for _, pattern := range []string{"rm -rf .", "rm -rf ~", "rm -rf /", "rm -rf *", "rm -rf .git"} {
		if strings.HasPrefix(cmdLower, pattern) {
			return "mass_deletion"
		}
	}

	// rm -rf of source/project directories (more specific than general dir deletion)
	for _, pattern := range []string{
		"rm -rf src/", "rm -rf src ", "rm -rf lib/", "rm -rf lib ",
		"rm -rf app/", "rm -rf app ", "rm -rf pkg/", "rm -rf pkg ",
		"rm -rf tests/", "rm -rf tests ", "rm -rf spec/", "rm -rf spec ",
		"rm -rf include/", "rm -rf include ", "rm -rf pages/", "rm -rf pages ",
		"rm -rf components/", "rm -rf components ",
	} {
		if strings.HasPrefix(cmdLower, pattern) {
			return "source_code_destruction"
		}
	}

	// rm -rf of arbitrary directories (general directory deletion)
	// Check this last, after more specific patterns above
	if (strings.HasPrefix(cmdLower, "rm -rf ") || strings.HasPrefix(cmdLower, "rm -fr ")) && !isSafeRmRfPrefix(cmdLower) {
		return "directory_deletion"
	}

	if strings.HasPrefix(cmdLower, "sudo ") {
		return "privilege_escalation"
	}
	if strings.Contains(cmd, "chmod 777") || strings.Contains(cmd, "chmod 666") {
		return "insecure_permissions"
	}
	// Pipe to any shell or script interpreter — arbitrary code execution via pipe
	if isPipeToShell(cmd) {
		return "remote_code_execution"
	}
	if strings.HasPrefix(cmdLower, "eval ") || cmd == "eval" {
		return "arbitrary_code_execution"
	}
	if strings.HasPrefix(cmdLower, "git push --force") || strings.HasPrefix(cmdLower, "git push -f") {
		return "destructive_git_operation"
	}
	if strings.HasPrefix(cmdLower, "git branch -d") || strings.HasPrefix(cmdLower, "git branch -D") {
		return "destructive_git_operation"
	}
	if strings.Contains(cmd, "() { :|: }") || strings.Contains(cmd, "fork bomb") {
		return "system_instability"
	}
	if strings.HasPrefix(cmdLower, "killall -9") {
		return "system_instability"
	}
	if strings.HasPrefix(cmd, "mkfs") {
		return "disk_destruction"
	}
	if strings.HasPrefix(cmd, "fdisk") || strings.HasPrefix(cmd, "parted") || strings.HasPrefix(cmd, "gparted") {
		return "disk_destruction"
	}
	if strings.Contains(cmd, "dd if=/dev/zero") || strings.Contains(cmd, "dd if=/dev/urandom") ||
		strings.Contains(cmd, "dd of=/dev/sda") || strings.Contains(cmd, "dd of=/dev/sdb") ||
		strings.Contains(cmd, "dd of=/dev/nvme") {
		return "disk_destruction"
	}
	// Output redirection to system directories
	for _, dir := range []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/opt/", "/boot/", "/root/"} {
		if strings.Contains(cmd, "> "+dir) || strings.Contains(cmd, ">> "+dir) {
			return "system_integrity"
		}
	}
	if (strings.Contains(cmd, "> /dev/") || strings.Contains(cmd, ">> /dev/")) &&
		!strings.Contains(cmd, "> /dev/null") && !strings.Contains(cmd, ">> /dev/null") &&
		!strings.Contains(cmd, "> /dev/stdout") && !strings.Contains(cmd, ">> /dev/stdout") &&
		!strings.Contains(cmd, "> /dev/stderr") && !strings.Contains(cmd, ">> /dev/stderr") {
		return "system_integrity"
	}

	// Fall back to generic critical system operation for anything caught by isCriticalSystemOperation
	if isCritical {
		return "critical_system_operation"
	}

	return ""
}
