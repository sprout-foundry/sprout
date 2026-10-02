//go:build !js

package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// isServiceMode returns true when sprout is running as a managed system
// service (systemd, launchd). In service mode, terminal prompts and
// "Press Ctrl+C" messages are suppressed since there is no interactive
// terminal.
func isServiceMode() bool {
	return configuration.GetEnvSimple("SERVICE") == "1"
}

var queryInProgress atomic.Bool

func setQueryInProgress(active bool) {
	queryInProgress.Store(active)
}

func isQueryInProgress() bool {
	return queryInProgress.Load()
}

func ensureContinuationSessionID(chatAgent *agent.Agent) string {
	if chatAgent == nil {
		return ""
	}
	sessionID := strings.TrimSpace(chatAgent.GetSessionID())
	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		chatAgent.SetSessionID(sessionID)
	}
	return sessionID
}

func printContinuationHint(chatAgent *agent.Agent) {
	sessionID := ensureContinuationSessionID(chatAgent)
	if sessionID == "" {
		return
	}
	fmt.Printf("To Continue: `sprout agent --session-id %s`\n", sessionID)
}

// printKeyboardHelp is a convenience wrapper that writes to stderr.
// Triggered by typing `?` alone at the idle prompt. Writes to stderr
// so it doesn't interleave with stdout-bound model output if the user
// pipes the session.
func printKeyboardHelp() {
	writeKeyboardHelp(os.Stderr)
}

// writeKeyboardHelp emits a compact, two-column reference of the keys
// the CLI exposes. Covers every binding the InputReader / SteerInputReader
// actually interpret — these features exist but were previously
// undocumented outside of code comments, making them effectively
// undiscoverable. Accepts a writer so tests can capture output.
func writeKeyboardHelp(w io.Writer) {
	colorOn := envutil.ResolveColorPreference(true)
	dim, reset := "", ""
	if colorOn {
		dim, reset = "\033[2m", "\033[0m"
	}
	rows := [][2]string{
		{"Editing (prompt and steer box alike)", ""},
		{"  Ctrl+A / Ctrl+E", "move cursor to line start / end"},
		{"  Ctrl+B / Ctrl+F", "move cursor back / forward one char"},
		{"  Ctrl+Left / Ctrl+Right", "move cursor back / forward one word"},
		{"  Alt+B / Alt+F", "move cursor back / forward one word"},
		{"  Ctrl+U / Ctrl+K", "cut to start / end of line"},
		{"  Ctrl+W / Alt+D", "cut the previous / next word"},
		{"  Ctrl+Y", "paste the last cut text"},
		{"  Ctrl+_ (Ctrl+/)", "undo"},
		{"  Ctrl+R", "reverse-search history"},
		{"  Ctrl+X Ctrl+E", "edit input in $EDITOR"},
		{"  @path", "complete a file path (Tab accepts)"},
		{"  Paste", "auto-detected; images attach"},
		{"", ""},
		{"Idle prompt", ""},
		{"  Enter", "send message to the agent"},
		{"  Alt+Enter / Shift+Enter", "insert newline (compose multi-line input)"},
		{"  Tab", "autocomplete /commands"},
		{"  ↑ / ↓", "recall prior prompts"},
		{"  Ctrl+L", "clear the screen"},
		{"  Ctrl+C", "clear line (press twice to exit)"},
		{"  Ctrl+D", "exit session (or delete next char)"},
		{"  Right-click", "context menu (cut/copy/paste)"},
		{"  /<cmd>", "slash command — type /help to list all"},
		{"  !<cmd>", "run a shell command directly"},
		{"  ?", "this help"},
		{"  exit / quit", "end session + print summary"},
		{"", ""},
		{"Steer box (while a turn is running)", ""},
		{"  Enter", "send mid-turn steer (default)"},
		{"  Tab", "toggle steer ↔ queue (queue auto-runs at turn end)"},
		{"  ↑ / ↓", "pull back un-picked steer; else recall prior steers"},
		{"  Ctrl+]", "cycle /command completions"},
		{"  Ctrl+L", "repaint the box"},
		{"  Esc", "clear the input (Ctrl+_ brings it back)"},
		{"  Ctrl+C", "interrupt the current turn"},
	}
	fmt.Fprintln(w)
	console.GlyphInfo.Fprintf(w, "Keyboard help")
	for _, r := range rows {
		if r[0] == "" {
			fmt.Fprintln(w)
			continue
		}
		if r[1] == "" {
			// Section header — bold if color is on.
			if colorOn {
				fmt.Fprintf(w, "  \033[1m%s%s\n", r[0], reset)
			} else {
				fmt.Fprintf(w, "  %s\n", r[0])
			}
			continue
		}
		// Two-column row. Align the description column at fixed width
		// so the descriptions stack visually.
		fmt.Fprintf(w, "  %-26s %s%s%s\n", r[0], dim, r[1], reset)
	}
	fmt.Fprintln(w)
}
