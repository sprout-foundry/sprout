//go:build !js

package cmd

import (
	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// SetupAgentEvents configures the agent for event-driven output routing.
// The OutputRouter handles dual-path delivery (EventBus + terminal)
// so no separate streaming callback is needed here. This function ensures
// the agent's output router is wired to the event bus for WebUI subscribers.
//
// When indicator is non-nil, the streaming callback also stops it on the
// first chunk so any "Thinking…" spinner is cleared before tokens appear.
func SetupAgentEvents(chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) {
	// Ensure the output router is connected to the event bus.
	// When WebUI is active, events flow to both terminal and WebUI.
	// When WebUI is inactive, events only flow to terminal.
	if router := chatAgent.OutputRouter(); router != nil {
		router.SetEventBus(eventBus)
		// SP-056: Resolve reasoning display mode. --reasoning takes
		// precedence over the legacy --show-reasoning-terminal flag.
		// Modes: "hidden" (no reasoning in terminal), "fold" (collapsed
		// token count — the default for interactive REPL), "full" (raw
		// reasoning text streams to terminal). The fold callback itself
		// is wired by the REPL loop in agent_mode_interactive.go.
		reasoningMode := agentReasoningMode
		if reasoningMode == "" {
			if agentShowReasoningTerminal {
				reasoningMode = "full"
			}
		}
		router.SetReasoningTerminalEnabled(reasoningMode == "full")
		// SP-056: Initialize the reasoning fold when mode is "fold".
		if reasoningMode == "fold" {
			currentReasoningFold = console.NewReasoningFold(indicator)
		} else {
			currentReasoningFold = nil
		}
	}

	// Set a simple streaming callback for direct terminal output of
	// assistant text. The OutputRouter's RouteStreamChunk publishes
	// the event AND calls this callback — no duplicate events or writes.
	//
	// Routing: if a per-turn AssistantTurnRenderer is active (set up by
	// the REPL loop), the chunk goes through it for indent + segment
	// tracking. Otherwise it falls back to raw fmt.Print (non-REPL
	// callers like queue mode).
	//
	// Assistant prose flows verbatim end-to-end: the terminal handles
	// soft-wrap on long lines. We deliberately do NOT clamp line length
	// here — prior versions truncated lines beyond `terminalWidth × 2`,
	// which clipped long prose paragraphs that lacked `\n` breaks
	// ("text being shown to the user shouldn't be cut off"). Tool
	// results don't reach this callback (they route via RouteAgentMessage
	// / RouteTerminalOnly), so there's no blob-output risk on this path.
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			// Suppress streaming output while an interactive prompt
			// (security approval, edit review) is on screen. Without
			// this, prose chunks clobber the picker rendering.
			if clihooks.IsStreamingSuspended() {
				return
			}
			if chunk != "" {
				// CompareAndSwap: only the FIRST non-empty chunk records
				// the ttft. Subsequent chunks are a no-op so reading the
				// timestamp later yields "first token landed at X".
				cliui.NoteFirstStreamChunk()
				// SP-056: Resolve any active reasoning fold on the first assistant
				// text chunk so the fold line is finalized before prose appears.
				if fold := currentReasoningFold; fold != nil && fold.IsActive() {
					fold.Resolve()
				}
			}
			// A browser is watching the Web UI — hand off there instead of
			// duplicating the token stream in the terminal. Print one handoff
			// line per turn and stay quiet.
			if chatAgent.HasActiveWebUIClients() {
				showWebUIHandoffOnce(indicator)
				return
			}
			indicator.Stop()
			if r := currentTurnRenderer.Load(); r != nil {
				// First prose chunk of a turn: the cursor is wherever the
				// indicator's Stop() left it on the shared TTY cursor
				// (col 0 of the cleared indicator row). Force a fresh
				// line so the renderer can safely emit indent + text
				// without those characters landing mid-line and
				// overwriting the indicator's residue. Once is enough —
				// subsequent chunks of the same stream continue from
				// the row we just moved to. Without this, the leading
				// 4-10 chars of the streamed prose collide with the
				// previous stderr writer and the first prose line
				// visibly starts mid-word.
				//
				// Skip the separator when the renderer's cursor is
				// already on a fresh row — true after reasoning ran
				// first (endReasoningLocked advanced past the summary
				// \n) and after any completed-line prose. Injecting
				// another \n there produces a spurious blank line.
				//
				// Also skip when a reasoning header is still active:
				// endReasoningLocked (called by WriteChunk below)
				// rewrites that header row in-place via \r\033[K.
				// Advancing to a fresh row first would orphan the
				// "▽ Thinking…" header and place the summary on the
				// wrong row.
				//
				// The separator is emitted inside WriteChunkWithSeparator's
				// LockOutput section so a concurrent footer draw's
				// DECSC/DECRC can't undo the cursor advance between the
				// \n and the chunk text.
				needsSeparator := false
				if chunk != "" && firstProseChunk.CompareAndSwap(false, true) && !r.CursorOnFreshRow() && !r.ReasoningActive() {
					needsSeparator = true
				}
				r.WriteChunkWithSeparator(chunk, needsSeparator)
				return
			}
			// Between turns: route through PrintExternal so background
			// messages (security cautions, late tool logs) don't corrupt
			// the active input line. When ReadLine is active, PrintExternal
			// clears the input line, prints the message, and redraws the
			// prompt + buffer below it — all under the console output lock.
			// When no ReadLine is active, it falls back to fmt.Print.
			console.PrintExternal(chunk)
		})
	}
}

// currentReasoningFold holds the ReasoningFold instance for the current
// session when reasoningMode == "fold". Created once at startup in
// SetupAgentEvents and reused across turns. Accessed from agent_mode_interactive.go
// and pkg/cliui/terminal_subscriber.go (extracted from cmd/ in SP-120 phase 2c).
var currentReasoningFold *console.ReasoningFold
