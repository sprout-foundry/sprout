package tools

import (
	"regexp"
	"strings"
)

// These checks are lexical and platform-neutral: the command string is what
// is classified, and a Windows host (cmd.exe fallback, Git Bash, or an
// agent invoking powershell) is where such commands actually run.

// windowsDrivePathAsSlash rewrites a drive-absolute Windows path
// ("C:\Users\x", "c:/users/x") to its volume-less slash form ("/Users/x") so
// the POSIX prefix rules (home directories safe, everything else a system
// path) apply to it. ok is false for any other shape.
func windowsDrivePathAsSlash(p string) (string, bool) {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "", false
	}
	if c := p[0] | 0x20; c < 'a' || c > 'z' {
		return "", false
	}
	return strings.ReplaceAll(p[2:], `\`, "/"), true
}

func isUNCPath(p string) bool {
	return strings.HasPrefix(p, `\\`)
}

// windowsDeletionPrefixes are cmd.exe / PowerShell deletions, the analogue
// of rm: they prompt rather than fall through to the Safe default.
var windowsDeletionPrefixes = []string{"del ", "erase ", "rd /s", "rmdir /s", "remove-item "}

func isWindowsDeletion(cmdLower string) bool {
	for _, prefix := range windowsDeletionPrefixes {
		if strings.HasPrefix(cmdLower, prefix) {
			return true
		}
	}
	return false
}

var windowsFormatDrivePattern = regexp.MustCompile(`^format(\.com)?\s+[a-z]:`)

// isWindowsDiskDestruction matches the mkfs/fdisk analogues.
func isWindowsDiskDestruction(cmdLower string) bool {
	return windowsFormatDrivePattern.MatchString(cmdLower) || cmdLower == "diskpart" || strings.HasPrefix(cmdLower, "diskpart ")
}
