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
	r := console.NewAssistantTurnRenderer(
		GetTerminalWidth(),
		console.NewMarkdownFormatter(true, true),
	)
	currentTurnRenderer.Store(r)
	firstProseChunk.Store(false)
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
	r.FinalizeAtTurnEnd()
	currentTurnRenderer.Store(nil)
}
