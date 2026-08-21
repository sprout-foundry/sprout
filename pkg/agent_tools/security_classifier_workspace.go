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
// all yield their path arguments.
var offWorkspacePathPattern = regexp.MustCompile(`(?:^|[\s|&;()<>'"=])(/[^\s|&;<>\"']*|~/?[^\s|&;<>\"']*)`)

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
// resolves outside workspaceRoot, /tmp, or extraAllowed. Lexical only —
// no filesystem access, so it cannot be fooled by symlink swaps between
// check and execution (the approval dialog itself names the raw paths).
func offWorkspacePathInCommand(cmd, workspaceRoot string, extraAllowed []string) bool {
	wsAbs := absPathLexical(workspaceRoot)
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
		if isUnderAny(resolved, wsAbs, extraAllowed) {
			continue
		}
		// Relative ../ escapes: resolve against the workspace root.
		if !filepath.IsAbs(resolved) {
			if isUnderAny(filepath.Join(wsAbs, resolved), wsAbs, extraAllowed) {
				continue
			}
		}
		return true
	}
	// ../ escapes anywhere in a relative token (leading or mid-path, e.g.
	// "sub/../../other"), resolved lexically against the workspace root.
	if wsAbs != "" {
		for _, field := range strings.Fields(cmd) {
			clean := strings.Trim(field, `"'`)
			if !strings.Contains(clean, "../") {
				continue
			}
			resolvedField := filepath.Clean(filepath.Join(wsAbs, clean))
			if !isUnderAny(resolvedField, wsAbs, extraAllowed) {
				return true
			}
		}
	}
	return false
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
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}

func isTmpPath(p string) bool {
	return p == "/tmp" || strings.HasPrefix(p, "/tmp/")
}

// absPathLexical cleans a path and makes it absolute relative to the
// process cwd when relative. Returns "" for empty input.
func absPathLexical(p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		if cwd, err := os.Getwd(); err == nil {
			p = filepath.Join(cwd, p)
		}
	}
	return filepath.Clean(p)
}

// isUnderAny reports whether path equals or sits under any of the roots.
func isUnderAny(path string, root string, extra []string) bool {
	if root != "" && (path == root || strings.HasPrefix(path, root+string(filepath.Separator))) {
		return true
	}
	for _, ex := range extra {
		if ex == "" {
			continue
		}
		if path == ex || strings.HasPrefix(path, ex+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
