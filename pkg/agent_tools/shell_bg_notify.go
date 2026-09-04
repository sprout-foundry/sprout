package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// maxCompletionTail bounds the output preview embedded in a background
// completion notification. The full output stays retrievable via
// check_background; the tail exists so a reaped or unreadable session
// doesn't leave the agent with nothing but a dangling session ID.
const maxCompletionTail = 2000

// formatShellBgCompletion renders the completion notice. The output tail
// is appended when available; the check_background pointer is kept so the
// agent can still fetch the full output.
func formatShellBgCompletion(sessionID string, exitCode int, outputTail string) string {
	var statusPart string
	switch exitCode {
	case BgExitNone:
		statusPart = "ended without a completion report (session was closed, reaped, or the PTY died before finishing)"
	case BgExitStopped:
		statusPart = "was stopped"
	default:
		statusPart = fmt.Sprintf("completed with exit code %d", exitCode)
	}
	msg := fmt.Sprintf("Background session %s %s.", sessionID, statusPart)
	if outputTail != "" {
		msg += fmt.Sprintf("\nLast output (up to %d bytes):\n%s\n", maxCompletionTail, outputTail)
	}
	msg += fmt.Sprintf("\nUse shell_command(check_background=%q) to see full output.", sessionID)
	return msg
}

// tailOfSessionOutput reads the trailing slice of a background session's
// output, aligned to a line boundary when possible. Best-effort: any
// error (session already reaped, file removed) yields "".
func tailOfSessionOutput(ctx context.Context, sessionID string) string {
	if tm := TerminalManagerFromContext(ctx); tm != nil {
		output, err := tm.GetBackgroundOutput(sessionID)
		if err != nil {
			return ""
		}
		return tailString(output, maxCompletionTail)
	}
	if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
		proc, exists := bpm.GetProcess(sessionID)
		if !exists {
			return ""
		}
		data, err := os.ReadFile(proc.GetOutputPath())
		if err != nil {
			return ""
		}
		return tailString(string(data), maxCompletionTail)
	}
	return ""
}

// tailString returns the last n bytes of s, shifted forward to the first
// newline when the cut lands mid-line (the partial first fragment is
// rarely useful in a preview).
func tailString(s string, n int) string {
	if len(s) <= n {
		return strings.TrimRight(s, "\n")
	}
	cut := len(s) - n
	if idx := strings.IndexByte(s[cut:], '\n'); idx >= 0 {
		cut += idx + 1
	}
	return strings.TrimRight(s[cut:], "\n")
}
