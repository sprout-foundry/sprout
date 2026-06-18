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

// writeKeyboardHelp emits a compact, two-column reference of the
// non-obvious keys the CLI exposes — primarily the steer-panel keys
// added by SP-055 since the rest of the bindings (slash commands,
// exit) are documented in the welcome banner and `/help`. Accepts a
// writer so tests can capture output.
func writeKeyboardHelp(w io.Writer) {
	colorOn := envutil.ResolveColorPreference(true)
	dim, reset := "", ""
	if colorOn {
		dim, reset = "\033[2m", "\033[0m"
	}
	rows := [][2]string{
		{"Steer panel (while a turn is running)", ""},
		{"  Enter", "send mid-turn steer (default)"},
		{"  Tab", "toggle steer ↔ queue mode"},
		{"  ↑ / ↓", "recall prior steer messages"},
		{"  Esc", "clear the input"},
		{"  Ctrl+C", "interrupt the current turn"},
		{"", ""},
		{"Idle prompt", ""},
		{"  /<cmd>", "slash command (/help, /commit, /persona, …)"},
		{"  ?", "this help"},
		{"  exit / quit", "end session + print summary"},
		{"  Ctrl+C × 2", "force quit"},
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
		fmt.Fprintf(w, "  %-18s %s%s%s\n", r[0], dim, r[1], reset)
	}
	fmt.Fprintln(w)
}
