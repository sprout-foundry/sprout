//go:build !js

package cmd

import (
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// When a browser is connected to the Web UI, the full assistant stream and
// tool-call output render there, so duplicating them token-by-token in the
// terminal is just noise. Instead the CLI stays quiet for the turn and prints a
// single handoff line pointing at the Web UI. These helpers coordinate that:
// the terminal output paths consult HasActiveWebUIClients() and call
// showWebUIHandoffOnce, which fires at most once per turn (reset by ProcessQuery
// via resetWebUIHandoff).

var (
	webUIDisplayURL           atomic.Pointer[string]
	webUIHandoffShownThisTurn atomic.Bool
)

// setWebUIDisplayURL records the address shown in the handoff line. Called once
// the web server is listening and its URL is known.
func setWebUIDisplayURL(url string) {
	webUIDisplayURL.Store(&url)
}

// resetWebUIHandoff clears the per-turn guard so the handoff line can appear
// once for the next turn. Called at the start of ProcessQuery.
func resetWebUIHandoff() {
	webUIHandoffShownThisTurn.Store(false)
}

// showWebUIHandoffOnce prints the "output is in the Web UI" line at most once
// per turn and stops the thinking spinner so it doesn't spin against suppressed
// output. No-op after the first call within a turn.
func showWebUIHandoffOnce(indicator *console.ActivityIndicator) {
	if webUIHandoffShownThisTurn.Swap(true) {
		return
	}
	if indicator != nil {
		indicator.Stop()
	}
	msg := "Web UI connected · output streaming in the browser"
	if u := webUIDisplayURL.Load(); u != nil && *u != "" {
		msg = "Web UI connected · output streaming at " + *u
	}
	console.GlyphInfo.Printf("  %s", msg)
}
