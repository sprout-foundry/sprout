//go:build !js

package cmd

import (
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// currentTurnRenderer holds the AssistantTurnRenderer for the in-progress
// REPL turn (or nil between turns / outside the REPL). The streaming
// callback registered in SetupAgentEvents loads from this pointer on each
// chunk so per-turn renderers can be swapped without re-registering the
// callback. Safe because only one turn is active at a time in a CLI REPL.
var currentTurnRenderer atomic.Pointer[console.AssistantTurnRenderer]

// currentResizeUnsub holds the deregistration function for the active turn's
// resize subscriber. When the terminal is resized mid-turn, the subscriber
// calls SetTerminalWidth on the active renderer so tables and horizontal rules
// use the current width instead of the stale snapshot from beginTurn. Cleared
// in endTurn.
var currentResizeUnsub atomic.Pointer[func()]

// firstProseChunk tracks whether the streaming callback has emitted the
// first prose chunk of the current turn. It's used by the streaming
// callback to inject a single `\n` between the activity-indicator's
// final `\r\033[K` (which leaves the shared TTY cursor on the cleared
// indicator row) and the renderer's first `WriteChunk`. Without that
// newline, the renderer's leading indent lands on the indicator's
// row and the first ~10 chars of the streamed prose collide with the
// indicator's cleared residue — the visible result is the first word
// of the response being partially or fully overwritten.
//
// Reset via beginTurn at the start of every turn.
var firstProseChunk atomic.Bool

// beginTurn wires up a fresh turn's renderer and resets the
// first-prose-chunk gate. Callers should use this instead of manually
// constructing the renderer and storing it, to avoid forgetting the
// firstProseChunk reset.
func beginTurn(chatAgent *agent.Agent) *console.AssistantTurnRenderer {
	width := GetTerminalWidth()
	r := console.NewAssistantTurnRenderer(
		width,
		console.NewMarkdownFormatter(true, true).SetWidth(width),
	)
	// Wire the status footer so the renderer can suppress its refresh
	// during active prose streaming — the root cause of the "scattered
	// characters" clobbering symptom.
	if footer := console.GetGlobalStatusFooter(); footer != nil {
		r.SetFooter(footer)
	}
	currentTurnRenderer.Store(r)
	firstProseChunk.Store(false)
	// Subscribe to terminal resize events so the renderer's width snapshot
	// (and its markdown formatter's table column clamping) updates live
	// when the terminal is resized mid-turn. Without this, tables and
	// horizontal rules use the stale width captured at turn start, causing
	// overflow or excessive padding after a resize.
	unsub := console.RegisterResizeSubscriber(func(width int) {
		r.SetTerminalWidth(width)
	})
	if prev := currentResizeUnsub.Swap(&unsub); prev != nil {
		// Overwriting a live subscription would leak it — the prior
		// renderer would keep receiving resizes forever.
		(*prev)()
	}
	if router := chatAgent.OutputRouter(); router != nil {
		router.SetExternalWriteHook(r.OnExternalWrite)
	}
	return r
}

// endTurn tears down the turn's renderer hooks and finalizes the
// renderer (markdown re-render if applicable). Safe to call with nil
// renderer. Callers should use this instead of manually tearing down
// to avoid forgetting hook cleanup.
func endTurn(chatAgent *agent.Agent, r *console.AssistantTurnRenderer) {
	if r == nil {
		return
	}
	if router := chatAgent.OutputRouter(); router != nil {
		router.SetExternalWriteHook(nil)
		router.SetReasoningCallback(nil)
	}
	// Deregister the resize subscriber before finalizing so a late resize
	// doesn't update a renderer that's about to be torn down.
	if unsub := currentResizeUnsub.Swap(nil); unsub != nil {
		(*unsub)()
	}
	r.FinalizeAtTurnEnd()
	currentTurnRenderer.Store(nil)
}
