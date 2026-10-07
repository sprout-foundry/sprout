//go:build !js

package cliui

// terminal_subscriber_events.go — the terminal subscriber event handlers
// (query started / completed, stream chunk, subagent activity, security
// prompt, todo update, agent message) and the event loop + entry point
// (runEventLoop, StartTerminalToolSubscriber). Split out of
// terminal_subscriber.go.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// HandleQueryStartedEvent processes a QueryStarted event (CLI-UX-5).
//
// When the LLM begins "thinking" — the gap between query submission and
// the first tool or streamed token — we show a contextual "thinking…"
// spinner so the terminal never looks frozen. The spinner is only started
// when no tool spinner is already active (the tool line is more
// informative) and suppressed entirely in compact mode.
//
// The spinner stops naturally when either:
//   - A StreamChunk with content_type arrives (assistant prose starts) →
//     HandleStreamChunkEvent clears it.
//   - A ToolStart fires → HandleToolStartEvent clears it and starts the
//     tool spinner.
func (s *TerminalSubscriberState) HandleQueryStartedEvent(indicator *console.ActivityIndicator) {
	if s.IsCompact() {
		return
	}
	// Don't clobber an active tool spinner — it's more informative.
	if indicator.IsActive() {
		return
	}
	indicator.Start("◐ thinking…")
	s.thinkingActive = true
}

// replOwnsTurnSummary is set while the interactive REPL runs: it prints its
// own per-turn line (PrintPerTurnSummary) from REPL-measured deltas, so the
// event-driven line would duplicate it — and the event's cost is the session
// total, not the turn's.
var replOwnsTurnSummary atomic.Bool

// SetREPLOwnsTurnSummary marks whether the interactive REPL prints the
// turn-end summary itself.
func SetREPLOwnsTurnSummary(owned bool) { replOwnsTurnSummary.Store(owned) }

// HandleQueryCompletedEvent processes a QueryCompleted event (CLI-UX-7).
//
// Prints a dim one-line turn summary so the user sees how long the turn
// took and how much it cost, without the clutter of a full metrics dump.
// Format:
//
//	✓ turn complete · 12.3s · $0.04
//
// Suppressed entirely in compact mode.
func (s *TerminalSubscriberState) HandleQueryCompletedEvent(data map[string]interface{}, indicator *console.ActivityIndicator) {
	if s.IsCompact() {
		return
	}
	// Clear any lingering spinner (thinking indicator left over from a
	// turn that ended without streaming prose — e.g. the model called
	// only tools then stopped).
	indicator.Stop()
	s.thinkingActive = false
	if replOwnsTurnSummary.Load() {
		return
	}

	durationMs := ReadEventInt64(data, "duration_ms")
	cost, _ := data["cost"].(float64)

	parts := []string{CompactDuration(time.Duration(durationMs) * time.Millisecond)}
	if cost > 0 {
		parts = append(parts, CompactCost(cost))
	}

	s.flushExternalWrite()
	line := fmt.Sprintf("%s%sturn complete · %s%s",
		console.GlyphSuccess.Prefix(),
		console.Esc(console.ColorDim),
		strings.Join(parts, " · "),
		console.Esc(console.ColorReset))
	fmt.Fprintln(os.Stderr, line)
}

// formatCostSummary renders a cost value for the turn-end summary line.
// Uses 4 decimal places for small amounts (common case), 2 for amounts
// >= $1. Kept as a local helper to avoid exporting the console package's
// internal cost formatter.
func formatCostSummary(cost float64) string {
	if cost >= 1.0 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.4f", cost)
}

// HandleStreamChunkEvent processes a StreamChunk event.
//
// If the chunk carries a content_type (assistant text), it breaks any
// pending tool-collapse run so the next ToolEnd prints a fresh row.
func (s *TerminalSubscriberState) HandleStreamChunkEvent(data map[string]interface{}, indicator *console.ActivityIndicator) {
	// CLI-UX-5: If the thinking spinner is active and assistant prose
	// begins streaming, stop the spinner so the prose renders cleanly.
	// Only chunks with content_type are assistant text/reasoning — those
	// that would land in the scroll region and overwrite the spinner row.
	if _, isText := data["content_type"].(string); isText {
		if s.thinkingActive {
			indicator.Stop()
			s.thinkingActive = false
		}
		// Assistant text or reasoning chunk landed in the
		// scroll region — any future tool-end can no longer
		// safely use ReplaceLastN to collapse onto the prior
		// row (the rows in between now hold model text).
		// Break the run; the next ToolEnd will print a fresh
		// row.
		s.run = nil
	}
}

// HandleSubagentActivityEvent processes a SubagentActivity event.
//
// "progress" status: cache the snapshot keyed by persona and refresh the
// footer so fleet-cost stays current.
// "completed"/"cancelled": emit a done summary line, clear progress cache,
// break the collapse run, and refresh the footer.
func (s *TerminalSubscriberState) HandleSubagentActivityEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	// SP-051-2d: render a one-line completion summary for
	// each subagent run. The spawn line ("↳ persona spawned
	// (provider · model)") already prints on the first
	// tool event from the subagent; the matching "done"
	// line below closes the bracket with the actual cost
	// of the delegation — tokens consumed, dollar cost,
	// and wall time — so the user can see at a glance how
	// expensive each subagent run was.
	status, _ := data["status"].(string)
	persona, _ := data["persona"].(string)
	switch status {
	case "progress":
		// SP-051-2e: live context update. Cache the snapshot
		// keyed by persona so the next tool line from this
		// subagent can append "· 12.3k/128k ctx". Don't
		// emit anything to the terminal directly — the
		// signal is meant to enrich existing rows, not add
		// new ones that would scroll past every 2s.
		s.progressMu.Lock()
		s.subagentProgress[persona] = SubagentProgressSnapshot{
			TokensUsed:  ReadEventInt(data, "tokens_used"),
			CtxUsed:     ReadEventInt(data, "context_used"),
			CtxMax:      ReadEventInt(data, "max_context_tokens"),
			Iteration:   ReadEventInt(data, "iteration"),
			LastUpdated: time.Now(),
		}
		s.progressMu.Unlock()
		// Refresh the footer so the cost field picks up the
		// fleet-cost delta even when no tool event is
		// firing (long shell_command inside the subagent).
		footer.Refresh()
	case "completed", "cancelled":
		tokens := ReadEventInt(data, "tokens_used")
		elapsedMs := ReadEventInt64(data, "elapsed_ms")
		cost, _ := data["cost"].(float64)
		reason, _ := data["reason"].(string)

		// Compact mode: suppress the subagent done line.
		// Still clean up progress tracking and refresh footer.
		if s.IsCompact() {
			s.progressMu.Lock()
			delete(s.subagentProgress, persona)
			s.progressMu.Unlock()
			s.run = nil
			footer.Refresh()
			return
		}

		// Subagents nest under the parent that spawned them.
		// Depth on the activity event isn't carried today, so
		// indent at the same level as the run_subagent tool
		// line — depth 1 — which is the common case. Deeper
		// nests fall back to a single indent rather than
		// guessing wrong.
		indicator.Stop()
		s.thinkingActive = false
		s.flushExternalWrite()
		fmt.Fprintln(os.Stderr, FormatSubagentDoneLine(persona, status, reason, tokens, cost, float64(elapsedMs)/1000.0))
		// Drop the cached progress for this persona once
		// it's done — the next spawn starts fresh.
		s.progressMu.Lock()
		delete(s.subagentProgress, persona)
		s.progressMu.Unlock()
		s.run = nil
		footer.Refresh()
	}
}

// HandleSecurityPromptEvent stops the spinner and breaks the collapse run
// when a prompt is about to render (security approval, security prompt,
// or ask_user). Subsequent activity re-starts the spinner naturally.
func (s *TerminalSubscriberState) HandleSecurityPromptEvent(indicator *console.ActivityIndicator) {
	// A prompt is about to render — stop any spinner so it
	// doesn't overwrite the prompt text. Subsequent activity
	// (next tool event, stream chunks) re-starts naturally.
	indicator.Stop()
	s.thinkingActive = false
	// Same row-layout invalidation as above.
	s.run = nil
}

// HandleTodoUpdateEvent renders the agent's todo list as a styled block
// in the scroll region. Breaks the collapse run and refreshes the footer.
func (s *TerminalSubscriberState) HandleTodoUpdateEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	// Compact mode: suppress todo block rendering — the user
	// doesn't see tool chrome so there's nothing to annotate.
	if s.IsCompact() {
		return
	}

	// Render the agent's current todo list as a styled block
	// in the scroll region so the user can see what's queued,
	// active, and done at a glance. The block lands AFTER the
	// ToolEnd line for todo_write (events fire in order), so
	// the layout reads:
	//   ✓ TodoWrite (5 tasks · 1 active) 0.0s
	//   ⓘ Todos · 5 total · 3 done · 1 active · 1 pending
	//      ✓ Investigate CLI todo tool rendering
	//      ✓ Audit stdin reading locations
	//      → Improve CLI todo rendering
	//      · Fix stdin reading with raw mode
	todosRaw, _ := data["todos"].([]interface{})
	indicator.Stop()
	s.thinkingActive = false
	s.flushExternalWrite()
	if len(todosRaw) == 0 {
		console.LockOutput()
		fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Todo list cleared")
		console.UnlockOutput()
		// Note: External callers must handle currentTurnRenderer themselves.
	} else {
		console.LockOutput()
		fmt.Fprintln(os.Stdout, FormatTodoListPanel(todosRaw))
		console.UnlockOutput()
		// Note: External callers must handle currentTurnRenderer themselves.
	}
	// Breaks any pending collapse run — the multi-line block
	// invalidates the row math the next ToolEnd would use.
	s.run = nil
	footer.Refresh()
}

// HandleAgentMessageEvent formats and prints an agent message (security
// caution, security loop, tool error, warning, or generic info) via
// console.PrintExternal. Breaks the collapse run and refreshes the footer.
func (s *TerminalSubscriberState) HandleAgentMessageEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	category, _ := data["category"].(string)
	message, _ := data["message"].(string)
	if message == "" {
		return
	}
	// Suppress tool_log messages — tool execution is displayed via the
	// dedicated tool_start/tool_end event handlers (spinner + end lines).
	// The tool_log agent_message event is kept for WebUI consumers but
	// must not produce terminal output or it clobbers the spinner with
	// duplicate "ⓘ executing tool" lines.
	if category == "tool_log" {
		return
	}
	indicator.Stop()
	s.thinkingActive = false
	s.flushExternalWrite()
	// Route through console.PrintExternal so the message
	// plays nicely with whichever reader owns the input:
	//   - Between turns (InputReader active): clears the
	//     input line, prints the message, redraws the
	//     prompt + buffer below it.
	//   - During turns (SteerInputReader active): writes
	//     into the scroll region above the pinned steer
	//     panel without disturbing it.
	//   - Neither active: falls through to fmt.Print.
	// PrintExternal takes outputMu internally; do NOT
	// wrap in console.LockOutput — the old code did that
	// around a raw fmt.Fprintf to os.Stderr, which wrote
	// bytes under the raw-mode cursor during a turn and
	// scrambled the user's in-progress input ("the input
	// broke" — the security caution landed where the
	// typed buffer was being rendered).
	//
	// PrintExternal auto-appends a trailing newline when
	// the message lacks one, so the format strings below
	// omit \n.
	console.PrintExternal(formatAgentNotice(category, message, console.StdoutColumns()))
	s.run = nil
	footer.Refresh()
}

// HandleProgressEvent renders the deterministic one-line summary of a
// SP-151 progress event in the scroll region (SP-151 §151c, item
// 151.6): milestone, verification, and completion events.
//
// progress_question events are deliberately NOT rendered: the CLI
// already shows the interactive ask_user prompt for the same decision
// (the ask_user_request / security-prompt path), so a second "Needs a
// decision" line would be redundant. The question template still exists
// for the web UI and webhooks (SP-151 §151c / §151d) — it just isn't
// printed in the terminal.
func (s *TerminalSubscriberState) HandleProgressEvent(evtType string, data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	// The story invariant: a progress_question's "Needs a decision" line is
	// deliberately not rendered — the interactive ask_user prompt already
	// shows the decision, so a second line would be redundant. Skip it
	// before the summary so the optional summarizer is never consulted for
	// an event the terminal does not print.
	if evtType == events.EventTypeProgressQuestion {
		return
	}
	summary := s.summarizeProgressEvent(evtType, data)
	if summary == "" {
		return
	}
	// Progress lines are informational; a completed run is a success
	// when verified and needs attention when it wasn't.
	glyph := console.GlyphInfo
	if evtType == events.EventTypeProgressComplete {
		if verified, _ := data["verified"].(bool); verified {
			glyph = console.GlyphSuccess
		} else {
			glyph = console.GlyphWarning
		}
	}
	// Same row-invalidation pattern as HandleAgentMessageEvent: stop
	// the spinner, flush buffered prose, then print through
	// console.PrintExternal — which takes outputMu internally, so do
	// NOT wrap in console.LockOutput. The explicit trailing newline
	// terminates the line on the bare fmt.Print fallback path (the
	// reader paths detect it and do not double it).
	indicator.Stop()
	s.thinkingActive = false
	s.flushExternalWrite()
	console.PrintExternal(glyph.Prefix() + summary + "\n")
	// The notice invalidates the collapse-run row math the next
	// ToolEnd would use.
	s.run = nil
	footer.Refresh()
}

// HandleLanguageGuardReplacementEvent renders a language_guard_replacement
// event: the guard repaired a reply that was already streamed, so the
// terminal must show the corrected text in place of the prose it already
// printed. The replacement lands on stdout as an external notice — the
// streaming turn's prose is already on the screen above it, so a "replaced"
// marker keeps the two texts distinguishable. A dim hint line follows,
// pointing at /original — the held original text stays in the session and
// the notice alone never carries it.
func (s *TerminalSubscriberState) HandleLanguageGuardReplacementEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	replacement, _ := data["replacement"].(string)
	if replacement == "" {
		return
	}
	indicator.Stop()
	s.thinkingActive = false
	s.flushExternalWrite()
	console.PrintExternal(console.WrapHanging(
		console.GlyphInfo.Prefix(), replacement, console.StdoutColumns()))
	console.PrintExternal(console.WrapHanging(
		console.GlyphDim.Prefix(), "Original text available with /original.", console.StdoutColumns()))
	s.run = nil
	footer.Refresh()
}

// runEventLoop is the goroutine body for the terminal tool subscriber.
// It selects on ctx cancellation and incoming events, dispatching each
// event type to the corresponding handler method.
// Note: eventBus.Unsubscribe is handled by the caller's deferred call in
// StartTerminalToolSubscriber, not here.
func (s *TerminalSubscriberState) runEventLoop(ctx context.Context, ch <-chan events.UIEvent, chatAgent *agent.Agent, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := evt.Data.(map[string]interface{})
			switch evt.Type {
			case events.EventTypeQueryStarted:
				s.HandleQueryStartedEvent(indicator)
			case events.EventTypeToolStart:
				s.HandleToolStartEvent(data, chatAgent, indicator)
			case events.EventTypeToolEnd:
				s.HandleToolEndEvent(data, chatAgent, indicator, footer)
			case events.EventTypeStreamChunk:
				s.HandleStreamChunkEvent(data, indicator)
			case events.EventTypeSubagentActivity:
				s.HandleSubagentActivityEvent(data, indicator, footer)
			case events.EventTypeSecurityApprovalRequest,
				events.EventTypeSecurityPromptRequest,
				events.EventTypeAskUserRequest:
				s.HandleSecurityPromptEvent(indicator)
			case events.EventTypeTodoUpdate:
				s.HandleTodoUpdateEvent(data, indicator, footer)
			case events.EventTypeAgentMessage:
				s.HandleAgentMessageEvent(data, indicator, footer)
			case events.EventTypeLanguageGuardReplacement:
				s.HandleLanguageGuardReplacementEvent(data, indicator, footer)
			case events.EventTypeProgressMilestone,
				events.EventTypeProgressVerification,
				events.EventTypeProgressComplete,
				events.EventTypeProgressQuestion:
				s.HandleProgressEvent(evt.Type, data, indicator, footer)
			case events.EventTypeQueryCompleted:
				s.HandleQueryCompletedEvent(data, indicator)
			}
		}
	}
}

// StartTerminalToolSubscriber subscribes a goroutine to the event bus that
// translates PublishToolStart / PublishToolEnd events into terminal spinner
// updates and ✓/✗ result lines. Runs until ctx is cancelled.
//
// Tools whose ToolConfig declares Interactive=true (e.g. ask_user) bypass
// the spinner entirely so their own prompt rendering isn't clobbered.
//
// Also stops the spinner on any prompt-request event (security approval,
// security prompt, ask_user) so prompts routed through the event bus get
// clean rendering with no spinner frames overwriting the prompt text. When
// footer is non-nil, it is refreshed on each ToolEnd so cost / context
// stay current as tools consume tokens.
//
// The chatAgent reference is used to resolve subagent personas to their
// effective provider/model so `run_subagent` lines can show which model
// will actually run the delegated task (subagents often use cheaper or
// faster models than the parent, and visibility into that matters).
func StartTerminalToolSubscriber(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator, footer *console.StatusFooter) func() {
	if eventBus == nil || indicator == nil {
		return func() {}
	}
	// Mark the OutputRouter: the subscriber owns agent_message rendering,
	// so RouteAgentMessage should skip the raw writeTerminalMessage fallback.
	if chatAgent != nil {
		if router := chatAgent.OutputRouter(); router != nil {
			router.SetTerminalSubscriberActive(true)
		}
	}
	subName := fmt.Sprintf("cli_tool_indicator_%d", time.Now().UnixNano())
	ch := eventBus.Subscribe(subName)

	// Read config manager live so output_verbosity changes via
	// /settings take effect mid-session without a restart. The
	// subscriber reads cfg.OutputVerbosity on each event via IsCompact().
	var configMgr *configuration.Manager
	if chatAgent != nil {
		configMgr = chatAgent.GetConfigManager()
	}

	state := NewTerminalSubscriberState(configMgr, chatAgent)
	go func() {
		defer eventBus.Unsubscribe(subName)
		state.runEventLoop(ctx, ch, chatAgent, indicator, footer)
	}()
	return state.ResetSpawnTurn
}

// formatAgentNotice lays out an agent message among the turn's tool lines:
// indented like them, and wrapped with a hanging indent so a long error
// stays readable instead of running across the terminal's hard wrap.
func formatAgentNotice(category, message string, cols int) string {
	var prefix string
	switch category {
	case "security_caution":
		prefix = console.GlyphWarning.Prefix()
		message = "[" + SecurityCautionLabel + "] " + strings.TrimPrefix(message, "[Security] ")
	case "security_loop":
		prefix = console.GlyphError.Prefix()
		message = "[" + SecurityLoopLabel + "] " + message
	case "tool_error":
		prefix = console.GlyphError.Prefix()
	case "warning":
		prefix = console.GlyphWarning.Prefix()
	default:
		prefix = console.GlyphInfo.Prefix()
	}
	return console.WrapHanging("  "+prefix, message, cols)
}
