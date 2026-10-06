package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// offWorkspacePathPattern lexically extracts absolute and ~-rooted path
// tokens from a shell command line. Shell separators and quotes act as
// token boundaries so "cat /etc/passwd|wc", ">/home/x", "--flag=/etc/y"
// all yield their path arguments. Drive-letter (C:\x, C:/x) and UNC
// (\\host\share) tokens are matched too so Windows paths are not skipped.
var offWorkspacePathPattern = regexp.MustCompile(`(?:^|[\s|&;()<>'"=])(/[^\s|&;<>\"']*|~/?[^\s|&;<>\"']*|[A-Za-z]:[\\/][^\s|&;<>\"']*|\\\\[^\s|&;<>\"']+)`)

var shellPathAlwaysAllowed = map[string]bool{
	"/dev/null":    true,
	"/dev/stdout":  true,
	"/dev/stderr":  true,
	"/dev/tty":     true,
	"/dev/zero":    true,
	"/dev/urandom": true,
}

// systemBinLibDirs are FHS executable/library trees. Commands routinely
// reference these by absolute path (/usr/bin/env, /bin/sh, ldd /usr/lib/...);
// they are not data targets, so they do not prompt. /etc and other data
// trees are deliberately absent.
var systemBinLibDirs = []string{
	"/bin", "/sbin", "/usr/bin", "/usr/sbin",
	"/usr/local/bin", "/usr/local/sbin",
	"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/local/lib",
}

func isSystemBinLibPath(p string) bool {
	for _, d := range systemBinLibDirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// ClassifyToolCallWithWorkspace augments ClassifyToolCall with workspace
// containment for shell commands. Any absolute or ~-rooted path (and any
// ../-relative escape) that resolves outside the workspace root, /tmp,
// or one of extraAllowed prompts for approval: Safe results escalate to
// Caution (ShouldPrompt), while already-prompting/blocking results are
// returned unchanged. Non-shell tools return the base classification.
func ClassifyToolCallWithWorkspace(toolName string, args map[string]interface{}, workspaceRoot string, extraAllowed ...string) SecurityResult {
	base := ClassifyToolCall(toolName, args)
	if toolName != "shell_command" {
		return base
	}
	cmd, _ := args["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return base
	}
	if base.Risk >= SecurityCaution || base.IsHardBlock {
		return base
	}
	if offWorkspacePathInCommand(cmd, workspaceRoot, extraAllowed) {
		return SecurityResult{
			Risk:         SecurityCaution,
			Reasoning:    "Command references paths outside the workspace root — approval required",
			ShouldPrompt: true,
			RiskType:     "filesystem_outside_workspace",
			Category:     RiskCategoryFileWrite,
		}
	}
	return base
}

// offWorkspacePathInCommand reports whether cmd references any path that
// resolves outside workspaceRoot, /tmp, or extraAllowed. Path resolution is
// lexical (no symlink following, so it cannot be fooled by symlink swaps
// between check and execution). The single filesystem touch is an existence
// probe of a rooted token's top-level directory, used to tell a real outside
// path (/etc/hosts) from a route string or search pattern that merely starts
// with "/" (grep -n "/api/git/"): a token whose top-level directory does not
// exist on this machine is not a file target.
func offWorkspacePathInCommand(cmd, workspaceRoot string, extraAllowed []string) bool {
	wsAbs := absPathLexical(workspaceRoot)
	allowed := make([]string, 0, len(extraAllowed))
	for _, ex := range extraAllowed {
		if ex != "" {
			allowed = append(allowed, absPathLexical(ex))
		}
	}
	for _, tok := range offWorkspacePathPattern.FindAllStringSubmatch(cmd, -1) {
		raw := strings.Trim(tok[1], `"'`)
		if raw == "" {
			continue
		}
		if shellPathAlwaysAllowed[strings.ToLower(raw)] {
			continue
		}
		if isTmpPath(raw) {
			continue
		}
		resolved := expandShellPathLexical(raw)
		if isSystemBinLibPath(resolved) {
			continue
		}
		if !isRootedPath(resolved) {
			if wsAbs == "" {
				return true
			}
			resolved = filepath.Join(wsAbs, resolved)
		}
		if !isUnderAny(absPathLexical(resolved), wsAbs, allowed) {
			// A rooted token whose first path component does not exist on this
			// machine cannot be a real file target — it is a route string,
			// search pattern (@see grep -n "/api/git/"), URL path, or similar
			// pattern argument. Ignoring it keeps such tokens from triggering a
			// spurious "outside the workspace root" prompt. Genuine outside
			// paths (e.g. /etc/hosts) have an existing top-level directory and
			// are still flagged; a path under a nonexistent tree is untraversable
			// and so is not a real escape either.
			//
			// The shortcut is skipped when the token contains a ".." segment:
			// such a token must be flagged regardless of whether its (cleaned)
			// top-level directory exists, because the shell resolves it against
			// the filesystem root, not the workspace — e.g.
			// `cat /nonexistent/../etc/hosts` reaches /etc/hosts even though
			// /nonexistent does not exist. Skipping the `..` case would reopen
			// exactly the escape this fix is meant to help catch.
			if isRootedPath(resolved) && !strings.Contains(raw, "..") && !topLevelDirExists(resolved) {
				continue
			}
			return true
		}
	}
	// ../ escapes anywhere in a relative token (leading or mid-path, e.g.
	// "sub/../../other"), resolved lexically against the workspace root.
	if wsAbs != "" {
		for _, field := range strings.Fields(cmd) {
			clean := strings.Trim(field, `"'`)
			if !strings.Contains(clean, "../") && !strings.Contains(clean, `..\`) {
				continue
			}
			resolvedField := filepath.Clean(filepath.Join(wsAbs, clean))
			if !isUnderAny(resolvedField, wsAbs, allowed) {
				return true
			}
		}
	}
	return false
}

// topLevelDirExists reports whether the first path component of a rooted
// path exists on this machine as a directory. It stats ONLY the top-level
// component (e.g. "/api" for "/api/git/") — never the full token — so it
// cannot be used to probe file contents, and a race on a deeper component
// cannot change the verdict. Used to tell a real outside path (whose top
// directory exists) from a route string or search pattern that merely
// starts with "/".
func topLevelDirExists(p string) bool {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		// Windows drive-rooted path: check the volume root itself.
		if len(p) >= 2 && p[1] == ':' {
			info, err := os.Stat(p[:2] + string(filepath.Separator))
			return err == nil && info.IsDir()
		}
		return false
	}
	trimmed := strings.TrimPrefix(p, "/")
	first := trimmed
	if idx := strings.IndexByte(trimmed, '/'); idx >= 0 {
		first = trimmed[:idx]
	}
	if first == "" {
		// "/" itself: the root always exists.
		return true
	}
	info, err := os.Stat(string(filepath.Separator) + first)
	return err == nil && info.IsDir()
}

// expandShellPathLexical expands a leading ~ to the user's home dir
// without touching the filesystem.
func expandShellPathLexical(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~`+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func isTmpPath(p string) bool {
	return p == "/tmp" || strings.HasPrefix(p, "/tmp/") || (isRootedPath(p) && isOSTempPath(p))
}

// isUnderAny reports whether path equals or sits under root or any of extra.
func isUnderAny(path string, root string, extra []string) bool {
	if pathWithin(path, root) {
		return true
	}
	for _, ex := range extra {
		if pathWithin(path, ex) {
			return true
		}
	}
	return false
}
