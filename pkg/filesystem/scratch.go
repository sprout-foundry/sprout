package filesystem

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ScratchDir is the agent scratch directory advertised in the system
// prompts for transient files (screenshots, logs, debugging output).
// It is /tmp/sprout, in lockstep with the prompt text and the security
// classifier's path allowlist — except on Windows, where Go would resolve
// "/tmp/sprout" to <drive>:\tmp\sprout while Git Bash maps /tmp to %TEMP%,
// so file tools and shell commands would see different directories. There
// it is %TEMP%/sprout in forward-slash form, a spelling Go, Git Bash and
// the prompts all accept.
var ScratchDir = scratchDir()

func scratchDir() string {
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(filepath.Join(os.TempDir(), "sprout"))
	}
	return "/tmp/sprout"
}

// EnsureScratchDir creates the agent scratch directory. Best-effort and
// idempotent: sandboxed environments with a read-only or namespace-
// isolated /tmp fail silently, which is acceptable — the system prompts
// tell the model to verify the directory is usable from shell_command
// and fall back to ./.scratch/ inside the workspace when it is not.
func EnsureScratchDir() {
	_ = os.MkdirAll(ScratchDir, 0o700)
}

// LocalizeScratchDir rewrites the /tmp/sprout scratch path in prompt text
// to this platform's ScratchDir. A no-op everywhere but Windows.
func LocalizeScratchDir(text string) string {
	if ScratchDir == "/tmp/sprout" {
		return text
	}
	return strings.ReplaceAll(text, "/tmp/sprout", ScratchDir)
}

// PosixTmpPath maps a drive-less "/tmp" path to the directory Git Bash
// calls /tmp — the user's temp dir — on Windows. Resolved literally,
// "/tmp/x" names <drive>:\tmp\x, so a file the agent wrote with a file
// tool was not the one its shell commands saw at /tmp/x. Reports false
// for every other path and on other platforms. The path is cleaned first,
// so "/tmp/../etc" is not treated as temp.
func PosixTmpPath(p string) (string, bool) {
	if runtime.GOOS != "windows" || p == "" || filepath.VolumeName(p) != "" {
		return "", false
	}
	slashed := filepath.ToSlash(filepath.Clean(p))
	if slashed == "/tmp" {
		return os.TempDir(), true
	}
	if rest, ok := strings.CutPrefix(slashed, "/tmp/"); ok {
		return filepath.Join(os.TempDir(), filepath.FromSlash(rest)), true
	}
	return "", false
}
