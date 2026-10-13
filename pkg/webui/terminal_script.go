//go:build !js

package webui

import (
	"fmt"
	"os"
	"strings"
)

// terminal_script.go materializes a shell command as a temp script so the
// PTY paths (ExecuteCommandAndWait, ExecuteCommandInBackground) send only a
// short launcher line instead of the whole command inline.
//
// Why: a PTY in canonical mode has a per-line input limit (~4KB on Linux and
// macOS). A wrapped command longer than that is silently truncated at the
// limit — the shell sees only the first statement of a chained command, the
// rest never executes, and neither the exit status nor the output reveals
// the loss. The same inline wrapping is why embedded newlines (heredocs,
// multi-line scripts) had to be rejected outright. Both failure modes
// disappear when the PTY only ever receives a short, bounded launcher line
// and the command itself travels through the filesystem.
//
// The script self-deletes when it finishes (success or failure), so nothing
// accumulates in the temp dir; a session that dies before the launcher runs
// leaks one small file, bounded by the OS temp dir.

// launcherMaxBytes caps the launcher line written to the PTY. Comfortably
// under the 4KB canonical-mode line limit; a launcher is fixed text plus a
// quoted temp path, so it stays far below this.
const launcherMaxBytes = 256

// needsSelfDelete reports whether the command references the script's own
// path, in which case the script must not delete itself on exit.
func needsSelfDelete(command string) bool {
	for _, ref := range []string{"$0", "${0}"} {
		if strings.Contains(command, ref) {
			return true
		}
	}
	return false
}

// writeCommandScript materializes command as an executable temp script that
// ends by echoing the completion sentinel with the command's exit code, and
// returns the script path plus the single-line launcher to write to the PTY.
//
// The sentinel is evaluated inside the script (not appended to the launcher)
// so `$code` semantics hold regardless of the session's interactive shell —
// fish, for one, has no `$?`. This preserves the guarantee the old inline
// wrap documented for `/bin/sh -c '… echo …:$?'`.
//
// The command runs in the shell's current working directory, exactly like
// the old inline path: the script never cds. An embedded `cd` affects only
// the command's subshell, as before.
func writeCommandScript(command, marker string) (scriptPath, launcher string, err error) {
	if strings.TrimSpace(command) == "" {
		return "", "", fmt.Errorf("command is empty")
	}

	f, err := os.CreateTemp("", "sprout-cmd-*.sh")
	if err != nil {
		return "", "", fmt.Errorf("create command script: %w", err)
	}
	path := f.Name()

	if _, writeErr := f.WriteString(buildScriptBody(command, marker)); writeErr != nil {
		f.Close()
		os.Remove(path)
		return "", "", fmt.Errorf("write command script: %w", writeErr)
	}
	if chmodErr := f.Chmod(0o700); chmodErr != nil {
		f.Close()
		os.Remove(path)
		return "", "", fmt.Errorf("chmod command script: %w", chmodErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		os.Remove(path)
		return "", "", fmt.Errorf("close command script: %w", closeErr)
	}

	launcher = "sh " + shQuote(path) + "\n"
	if len(launcher) > launcherMaxBytes {
		os.Remove(path)
		return "", "", fmt.Errorf("command launcher exceeds %d bytes", launcherMaxBytes)
	}
	return path, launcher, nil
}

// buildScriptBody renders the script content. The command is stored
// verbatim — newlines, quotes, and dollar expansions included — inside a
// single-quoted argument to `sh -c`, so its semantics match the old inline
// `/bin/sh -c '<escaped>'` byte-for-byte. The command's status is captured
// before anything else runs, echoed in the sentinel (double-quoted so the
// expansion happens; the marker itself is hex), and used as the script's
// exit status.
func buildScriptBody(command, marker string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	// Materialized by sprout: one launcher line in, full command executed.
	// Self-delete on exit unless the command references the script itself.
	if !needsSelfDelete(command) {
		b.WriteString("trap 'rm -f \"$0\"' EXIT\n")
	}
	b.WriteString("sh -c " + shQuote(command) + "\n")
	b.WriteString("code=$?\n")
	fmt.Fprintf(&b, "echo \"%s%s:$code\"\n", sentinelPrefix, marker)
	b.WriteString("exit $code\n")
	return b.String()
}

// shQuote single-quotes s for sh; a lone quote becomes the '\” escape.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
