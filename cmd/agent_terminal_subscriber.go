//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Security-broadcast labels rendered as bracketed prefixes inside the
// terminal subscriber. Kept as package constants so the glyph + label
// pair can't drift: the GlyphWarning prefix (⚠) already conveys "this is
// a warning", and the bracketed label carries the semantic category for
// grep + a11y tooling. CLI-B-2 extraction.
const (
	securityCautionLabel = "⚠️  SECURITY CAUTION"
	securityLoopLabel    = "🛑 SECURITY LOOP"
)

// terminalSubscriberState holds all mutable state for the terminal tool
// subscriber goroutine. Extracted from the closure variables of
// startTerminalToolSubscriber so the event loop can be broken into
// focused handler methods.
type terminalSubscriberState struct {
	spawnMu          sync.Mutex
	seenSpawn        map[string]bool
	run              *toolRunState
	pendingArgs      map[string]string
	progressMu       sync.Mutex
	subagentProgress map[string]subagentProgressSnapshot
}

// newTerminalSubscriberState initializes a fresh subscriber state with
// pre-allocated maps.
func newTerminalSubscriberState() *terminalSubscriberState {
	return &terminalSubscriberState{
		seenSpawn:        make(map[string]bool),
		pendingArgs:      make(map[string]string),
		subagentProgress: make(map[string]subagentProgressSnapshot),
	}
}

// resetSpawnTurn clears the per-turn spawn dedupe map so the next batch
// of subagents gets fresh announcements. Called by the REPL loop at the
// start of each user turn.
func (s *terminalSubscriberState) resetSpawnTurn() {
	s.spawnMu.Lock()
	s.seenSpawn = make(map[string]bool)
	s.spawnMu.Unlock()
}

// handleToolStartEvent processes a ToolStart event.
//
// Interactive tools bypass the spinner entirely. For all other tools:
// resolve any active reasoning fold, cache args for the matching ToolEnd,
// announce subagent spawns once per (depth, persona) per turn, and start
// the activity indicator with a context suffix when progress is available.
func (s *terminalSubscriberState) handleToolStartEvent(data map[string]interface{}, chatAgent *agent.Agent, indicator *console.ActivityIndicator) {
	name, _ := data["tool_name"].(string)
	if agent.IsInteractiveTool(name) {
		// Tool renders its own prompt — make sure any active
		// spinner is gone before the prompt lands.
		indicator.Stop()
		return
	}
	// SP-056-6a: Resolve any active reasoning fold on the first tool event
	// when reasoning ended but no assistant text arrived to trigger resolution.
	if fold := currentReasoningFold; fold != nil && fold.IsActive() {
		fold.Resolve()
	}
	args, _ := data["arguments"].(string)
	if id, _ := data["tool_call_id"].(string); id != "" && args != "" {
		s.pendingArgs[id] = args
	}
	depth := readEventDepth(data)
	persona := readEventPersona(data)
	// SP-051-2c: announce subagent spawn once per (depth,
	// persona) pair per turn, with provider/model so the user
	// can see which cheaper/faster model is doing the work.
	if depth > 0 && persona != "" {
		key := fmt.Sprintf("%d:%s", depth, persona)
		s.spawnMu.Lock()
		announce := !s.seenSpawn[key]
		if announce {
			s.seenSpawn[key] = true
		}
		s.spawnMu.Unlock()
		if announce {
			indicator.Stop()
			s.progressMu.Lock()
			spawnSnap, hasSpawnSnap := s.subagentProgress[persona]
			s.progressMu.Unlock()
			ctxMax := 0
			if hasSpawnSnap {
				ctxMax = spawnSnap.ctxMax
			}
			fmt.Fprintln(os.Stderr, formatSpawnLine(chatAgent, depth, persona, ctxMax))
		}
	}
	// Ensure the spinner lands on a fresh line so it never
	// overwrites partial streamed text. Stdout for parity
	// with how stream chunks were just printed.
	console.LockOutput()
	fmt.Fprintln(os.Stdout)
	console.UnlockOutput()
	// Notify the renderer that an external write consumed
	// one terminal row so physicalLines stays in sync.
	if r := currentTurnRenderer.Load(); r != nil {
		r.OnExternalWriteRows(1)
	}
	s.progressMu.Lock()
	snap, hasSnap := s.subagentProgress[persona]
	s.progressMu.Unlock()
	ctxSuffix := ""
	if hasSnap && depth > 0 {
		ctxSuffix = formatSubagentCtxSuffix(snap)
	}
	indicator.Start(formatToolStartLine(depth, persona, name, formatToolPreview(chatAgent, name, args)) + ctxSuffix)
}

// handleToolEndEvent processes a ToolEnd event.
//
// Interactive tools are skipped. For other tools: recover args from the
// ToolStart cache, collapse consecutive identical calls into a single
// in-place row (Phase 3), or emit a fresh end line. Refreshes the footer.
func (s *terminalSubscriberState) handleToolEndEvent(data map[string]interface{}, chatAgent *agent.Agent, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	name, _ := data["tool_name"].(string)
	if agent.IsInteractiveTool(name) {
		// No spinner was started; emit no result chrome.
		return
	}
	status, _ := data["status"].(string)
	var durationMs int64
	switch v := data["duration_ms"].(type) {
	case int64:
		durationMs = v
	case float64:
		durationMs = int64(v)
	}
	icon := console.GlyphSuccess.Prefix()
	if status != "completed" {
		icon = console.GlyphError.Prefix()
	}
	// ToolEnd doesn't carry arguments; recover them from
	// the ToolStart cache so the collapse-line preview
	// shows real paths instead of empty parens.
	args, _ := data["arguments"].(string)
	if args == "" {
		if id, _ := data["tool_call_id"].(string); id != "" {
			if cached, ok := s.pendingArgs[id]; ok {
				args = cached
				delete(s.pendingArgs, id)
			}
		}
	}
	depth := readEventDepth(data)
	persona := readEventPersona(data)
	preview := formatToolPreview(chatAgent, name, args)

	// Phase 3 collapse: if this end matches the prior run
	// (same name/depth/persona) AND less than 30s elapsed,
	// merge with the prior tool-end row instead of stacking
	// a new one. The 30s heuristic prevents collapse when
	// the model has streamed text between calls (which
	// would invalidate the row math).
	now := time.Now()
	if s.run != nil && s.run.matches(name, depth, persona) && now.Sub(s.run.lastEnd) < 30*time.Second {
		s.run.count++
		s.run.appendArg(preview)
		s.run.totalMs += durationMs
		s.run.lastEnd = now
		s.run.lastIcon = icon
		// 2 rows up: the spinner row (now cleared by
		// Stop) + the blank stdout newline emitted by
		// ToolStart + the previous tool-end row. The
		// indicator's Stop already cleared the spinner
		// row in place, so we walk past the blank line
		// and the previous end-line.
		indicator.ReplaceLastN(formatToolRunLine(
			s.run.depth, s.run.persona, s.run.lastIcon, s.run.name,
			s.run.count, s.run.argsTrail,
			float64(s.run.totalMs)/1000.0,
		), 2)
	} else {
		indicator.Replace(formatToolEndLine(depth, persona, icon, name,
			preview, float64(durationMs)/1000.0))
		s.run = &toolRunState{
			name:      name,
			depth:     depth,
			persona:   persona,
			count:     1,
			argsTrail: []string{preview},
			totalMs:   durationMs,
			lastIcon:  icon,
			lastEnd:   now,
		}
	}
	footer.Refresh()
}

// handleStreamChunkEvent processes a StreamChunk event.
//
// If the chunk carries a content_type (assistant text), it breaks any
// pending tool-collapse run so the next ToolEnd prints a fresh row.
func (s *terminalSubscriberState) handleStreamChunkEvent(data map[string]interface{}) {
	// Assistant text or reasoning chunk landed in the
	// scroll region — any future tool-end can no longer
	// safely use ReplaceLastN to collapse onto the prior
	// row (the rows in between now hold model text).
	// Break the run; the next ToolEnd will print a fresh
	// row.
	if _, isText := data["content_type"].(string); isText {
		s.run = nil
	}
}

// handleSubagentActivityEvent processes a SubagentActivity event.
//
// "progress" status: cache the snapshot keyed by persona and refresh the
// footer so fleet-cost stays current.
// "completed"/"cancelled": emit a done summary line, clear progress cache,
// break the collapse run, and refresh the footer.
func (s *terminalSubscriberState) handleSubagentActivityEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
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
		s.subagentProgress[persona] = subagentProgressSnapshot{
			tokensUsed:  readEventInt(data, "tokens_used"),
			ctxUsed:     readEventInt(data, "context_used"),
			ctxMax:      readEventInt(data, "max_context_tokens"),
			iteration:   readEventInt(data, "iteration"),
			lastUpdated: time.Now(),
		}
		s.progressMu.Unlock()
		// Refresh the footer so the cost field picks up the
		// fleet-cost delta even when no tool event is
		// firing (long shell_command inside the subagent).
		footer.Refresh()
	case "completed", "cancelled":
		tokens := readEventInt(data, "tokens_used")
		elapsedMs := readEventInt64(data, "elapsed_ms")
		cost, _ := data["cost"].(float64)
		reason, _ := data["reason"].(string)
		// Subagents nest under the parent that spawned them.
		// Depth on the activity event isn't carried today, so
		// indent at the same level as the run_subagent tool
		// line — depth 1 — which is the common case. Deeper
		// nests fall back to a single indent rather than
		// guessing wrong.
		indicator.Stop()
		fmt.Fprintln(os.Stderr, formatSubagentDoneLine(persona, status, reason, tokens, cost, float64(elapsedMs)/1000.0))
		// Drop the cached progress for this persona once
		// it's done — the next spawn starts fresh.
		s.progressMu.Lock()
		delete(s.subagentProgress, persona)
		s.progressMu.Unlock()
		s.run = nil
		footer.Refresh()
	}
}

// handleSecurityPromptEvent stops the spinner and breaks the collapse run
// when a prompt is about to render (security approval, security prompt,
// or ask_user). Subsequent activity re-starts the spinner naturally.
func (s *terminalSubscriberState) handleSecurityPromptEvent(indicator *console.ActivityIndicator) {
	// A prompt is about to render — stop any spinner so it
	// doesn't overwrite the prompt text. Subsequent activity
	// (next tool event, stream chunks) re-starts naturally.
	indicator.Stop()
	// Same row-layout invalidation as above.
	s.run = nil
}

// handleTodoUpdateEvent renders the agent's todo list as a styled block
// in the scroll region. Breaks the collapse run and refreshes the footer.
func (s *terminalSubscriberState) handleTodoUpdateEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
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
	if len(todosRaw) == 0 {
		console.LockOutput()
		fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Todo list cleared")
		console.UnlockOutput()
		// Notify the renderer that an external write consumed
		// one terminal row so physicalLines stays in sync.
		if r := currentTurnRenderer.Load(); r != nil {
			r.OnExternalWriteRows(1)
		}
	} else {
		rowCount := todoBlockRowCount(todosRaw)
		console.LockOutput()
		fmt.Fprintln(os.Stdout, formatTodoListBlock(todosRaw))
		console.UnlockOutput()
		// Notify the renderer that the multi-line todo block
		// consumed rowCount terminal rows so physicalLines
		// stays in sync for FinalizeAtTurnEnd.
		if r := currentTurnRenderer.Load(); r != nil {
			r.OnExternalWriteRows(rowCount)
		}
	}
	// Breaks any pending collapse run — the multi-line block
	// invalidates the row math the next ToolEnd would use.
	s.run = nil
	footer.Refresh()
}

// handleAgentMessageEvent formats and prints an agent message (security
// caution, security loop, tool error, warning, or generic info) via
// console.PrintExternal. Breaks the collapse run and refreshes the footer.
func (s *terminalSubscriberState) handleAgentMessageEvent(data map[string]interface{}, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
	category, _ := data["category"].(string)
	message, _ := data["message"].(string)
	if message == "" {
		return
	}
	indicator.Stop()
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
	var line string
	switch category {
	case "security_caution":
		line = fmt.Sprintf("%s[%s] %s", console.GlyphWarning.Prefix(), securityCautionLabel, message)
	case "security_loop":
		line = fmt.Sprintf("%s[%s] %s", console.GlyphError.Prefix(), securityLoopLabel, message)
	case "tool_error":
		line = fmt.Sprintf("%s%s", console.GlyphError.Prefix(), message)
	case "warning":
		line = fmt.Sprintf("%s%s", console.GlyphWarning.Prefix(), message)
	default:
		line = fmt.Sprintf("%s%s", console.GlyphInfo.Prefix(), message)
	}
	console.PrintExternal(line)
	s.run = nil
	footer.Refresh()
}

// runEventLoop is the goroutine body for the terminal tool subscriber.
// It selects on ctx cancellation and incoming events, dispatching each
// event type to the corresponding handler method.
// Note: eventBus.Unsubscribe is handled by the caller's deferred call in
// startTerminalToolSubscriber, not here.
func (s *terminalSubscriberState) runEventLoop(ctx context.Context, ch <-chan events.UIEvent, chatAgent *agent.Agent, indicator *console.ActivityIndicator, footer *console.StatusFooter) {
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
			case events.EventTypeToolStart:
				s.handleToolStartEvent(data, chatAgent, indicator)
			case events.EventTypeToolEnd:
				s.handleToolEndEvent(data, chatAgent, indicator, footer)
			case events.EventTypeStreamChunk:
				s.handleStreamChunkEvent(data)
			case events.EventTypeSubagentActivity:
				s.handleSubagentActivityEvent(data, indicator, footer)
			case events.EventTypeSecurityApprovalRequest,
				events.EventTypeSecurityPromptRequest,
				events.EventTypeAskUserRequest:
				s.handleSecurityPromptEvent(indicator)
			case events.EventTypeTodoUpdate:
				s.handleTodoUpdateEvent(data, indicator, footer)
			case events.EventTypeAgentMessage:
				s.handleAgentMessageEvent(data, indicator, footer)
			}
		}
	}
}

// startTerminalToolSubscriber subscribes a goroutine to the event bus that
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
func startTerminalToolSubscriber(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator, footer *console.StatusFooter) func() {
	if eventBus == nil || indicator == nil {
		return func() {}
	}
	subName := fmt.Sprintf("cli_tool_indicator_%d", time.Now().UnixNano())
	ch := eventBus.Subscribe(subName)
	state := newTerminalSubscriberState()
	go func() {
		defer eventBus.Unsubscribe(subName)
		state.runEventLoop(ctx, ch, chatAgent, indicator, footer)
	}()
	return state.resetSpawnTurn
}
