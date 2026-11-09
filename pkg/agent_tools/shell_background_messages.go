package tools

// Messages about background sessions, shared by every build: the browser
// build has no background sessions, but the shell tool's code that words
// them is compiled there too.

import (
	"fmt"
	"time"
)

// duplicateCommandMessage is what an agent sees instead of a second copy of
// a command that is already running. It names the session, says how long it
// has been running, and says plainly what the alternatives are — waiting,
// reading its output, or stopping it — without asking the agent to run the
// command again.
func duplicateCommandMessage(sessionID string, elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	if sessionID == "" {
		// A concurrent start was admitted and its session is not visible
		// yet. There is no ID to name, so say that instead of printing an
		// empty one the agent would pass to check_background.
		return "Not started: this command is already starting in this chat or directory. " +
			"Wait for that copy to appear in the background tasks and use shell_command check_background on it " +
			"rather than starting another."
	}
	return fmt.Sprintf(
		"Not started: this command is already running in this directory, in background session %s (started %s ago).\n\n"+
			"Do not run it again: a second copy would compete with the first for the machine.\n"+
			"- Wait for it to finish (blocks up to 10 minutes; repeat to keep waiting): shell_command check_background=\"%s\" wait_seconds=600\n"+
			"- Read its output so far: shell_command check_background=\"%s\"\n"+
			"- Stop it, to try a different approach: shell_command stop_background=\"%s\"",
		sessionID, formatSessionElapsed(elapsed), sessionID, sessionID, sessionID,
	)
}

// DuplicateCommandPrefix marks an error whose text is not a failure but a
// refusal to start a second copy of a command that is already running (see
// RunningBackgroundCommand). The shell tool turns it into a normal tool
// result naming the running session. It follows the
// COMMAND_PROMOTED_TO_BACKGROUND convention for errors carrying a session.
const DuplicateCommandPrefix = "COMMAND_ALREADY_RUNNING:"

// RunningBackgroundCommand is the exported form of duplicateCommandMessage,
// for start paths outside this package (the WebUI terminal manager) that
// refuse a duplicate before any process is created.
func RunningBackgroundCommand(sessionID string, elapsed time.Duration) string {
	return duplicateCommandMessage(sessionID, elapsed)
}

// formatSessionElapsed renders a session age for an agent-facing message:
// a whole number of seconds with its unit, so "0 seconds ago" is a real age
// rather than a rounded-away "0s".
func formatSessionElapsed(d time.Duration) string {
	secs := int64(d.Round(time.Second) / time.Second)
	switch {
	case secs == 1:
		return "1 second"
	case secs < 60:
		return fmt.Sprintf("%d seconds", secs)
	default:
		mins := secs / 60
		rest := secs % 60
		if rest == 0 {
			if mins == 1 {
				return "1 minute"
			}
			return fmt.Sprintf("%d minutes", mins)
		}
		if mins == 1 {
			return fmt.Sprintf("1 minute %d seconds", rest)
		}
		return fmt.Sprintf("%d minutes %d seconds", mins, rest)
	}
}
