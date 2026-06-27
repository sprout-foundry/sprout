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

	// SP-051-2c: per-turn dedupe of "↳ persona spawned" announcement lines.
	// We track which (depth, persona) pairs have already been announced for
	// the current turn; the returned reset func is invoked by the REPL loop
	// at the start of each user turn so the next batch of subagents gets
	// fresh announcements.
	var spawnMu sync.Mutex
	seenSpawn := make(map[string]bool)
	resetSpawn := func() {
		spawnMu.Lock()
		seenSpawn = make(map[string]bool)
		spawnMu.Unlock()
	}

	// Phase 3: tool-collapse state. Tracks the most recently completed
	// tool's (name, depth, persona) so consecutive identical calls
	// merge into "✓ read_file × N (foo, bar, baz)" instead of stacking
	// N rows. Reset by any inter-tool event that would invalidate the
	// row layout (currently: only sessions where < 30s elapsed since
	// the previous end qualify for the collapse).
	var run *toolRunState

	// pendingArgs caches the `arguments` JSON string from ToolStart
	// events keyed by tool_call_id so the corresponding ToolEnd can
	// recover the args for preview rendering. ToolEndEvent (in
	// pkg/events/events.go) does NOT carry arguments — so without this
	// cache the collapsed-run line rendered as "× N (, , )" with empty
	// parens. Entries clear on the matching ToolEnd to bound growth.
	pendingArgs := map[string]string{}

	// subagentProgress caches the latest progress snapshot per persona
	// so tool lines fired from depth>0 agents can append a "12.3k/128k
	// ctx" suffix without each tool event needing to ferry the values
	// itself. Updated by EventTypeSubagentActivity status=progress
	// events; cleared on completed/cancelled. Per-persona (not per-task)
	// because the CLI renders the depth+persona pair, not task IDs.
	var progressMu sync.Mutex
	subagentProgress := map[string]subagentProgressSnapshot{}

	go func() {
		defer eventBus.Unsubscribe(subName)
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
					name, _ := data["tool_name"].(string)
					if agent.IsInteractiveTool(name) {
						// Tool renders its own prompt — make sure any active
						// spinner is gone before the prompt lands.
						indicator.Stop()
						continue
					}
					// SP-056-6a: Resolve any active reasoning fold on the first tool event
					// when reasoning ended but no assistant text arrived to trigger resolution.
					if fold := currentReasoningFold; fold != nil && fold.IsActive() {
						fold.Resolve()
					}
					args, _ := data["arguments"].(string)
					if id, _ := data["tool_call_id"].(string); id != "" && args != "" {
						pendingArgs[id] = args
					}
					depth := readEventDepth(data)
					persona := readEventPersona(data)
					// SP-051-2c: announce subagent spawn once per (depth,
					// persona) pair per turn, with provider/model so the user
					// can see which cheaper/faster model is doing the work.
					if depth > 0 && persona != "" {
						key := fmt.Sprintf("%d:%s", depth, persona)
						spawnMu.Lock()
						announce := !seenSpawn[key]
						if announce {
							seenSpawn[key] = true
						}
						spawnMu.Unlock()
						if announce {
							indicator.Stop()
							progressMu.Lock()
							spawnSnap, hasSpawnSnap := subagentProgress[persona]
							progressMu.Unlock()
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
					fmt.Fprintln(os.Stdout)
					progressMu.Lock()
					snap, hasSnap := subagentProgress[persona]
					progressMu.Unlock()
					ctxSuffix := ""
					if hasSnap && depth > 0 {
						ctxSuffix = formatSubagentCtxSuffix(snap)
					}
					indicator.Start(formatToolStartLine(depth, persona, name, formatToolPreview(chatAgent, name, args)) + ctxSuffix)
				case events.EventTypeToolEnd:
					name, _ := data["tool_name"].(string)
					if agent.IsInteractiveTool(name) {
						// No spinner was started; emit no result chrome.
						continue
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
							if cached, ok := pendingArgs[id]; ok {
								args = cached
								delete(pendingArgs, id)
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
					if run != nil && run.matches(name, depth, persona) && now.Sub(run.lastEnd) < 30*time.Second {
						run.count++
						run.appendArg(preview)
						run.totalMs += durationMs
						run.lastEnd = now
						run.lastIcon = icon
						// 2 rows up: the spinner row (now cleared by
						// Stop) + the blank stdout newline emitted by
						// ToolStart + the previous tool-end row. The
						// indicator's Stop already cleared the spinner
						// row in place, so we walk past the blank line
						// and the previous end-line.
						indicator.ReplaceLastN(formatToolRunLine(
							run.depth, run.persona, run.lastIcon, run.name,
							run.count, run.argsTrail,
							float64(run.totalMs)/1000.0,
						), 2)
					} else {
						indicator.Replace(formatToolEndLine(depth, persona, icon, name,
							preview, float64(durationMs)/1000.0))
						run = &toolRunState{
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
				case events.EventTypeStreamChunk:
					// Assistant text or reasoning chunk landed in the
					// scroll region — any future tool-end can no longer
					// safely use ReplaceLastN to collapse onto the prior
					// row (the rows in between now hold model text).
					// Break the run; the next ToolEnd will print a fresh
					// row.
					if _, isText := data["content_type"].(string); isText {
						run = nil
					}
				case events.EventTypeSubagentActivity:
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
						progressMu.Lock()
						subagentProgress[persona] = subagentProgressSnapshot{
							tokensUsed:  readEventInt(data, "tokens_used"),
							ctxUsed:     readEventInt(data, "context_used"),
							ctxMax:      readEventInt(data, "max_context_tokens"),
							iteration:   readEventInt(data, "iteration"),
							lastUpdated: time.Now(),
						}
						progressMu.Unlock()
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
						progressMu.Lock()
						delete(subagentProgress, persona)
						progressMu.Unlock()
						run = nil
						footer.Refresh()
					}
				case events.EventTypeSecurityApprovalRequest,
					events.EventTypeSecurityPromptRequest,
					events.EventTypeAskUserRequest:
					// A prompt is about to render — stop any spinner so it
					// doesn't overwrite the prompt text. Subsequent activity
					// (next tool event, stream chunks) re-starts naturally.
					indicator.Stop()
					// Same row-layout invalidation as above.
					run = nil
				case events.EventTypeTodoUpdate:
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
						fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Todo list cleared")
					} else {
						fmt.Fprintln(os.Stdout, formatTodoListBlock(todosRaw))
					}
					// Breaks any pending collapse run — the multi-line block
					// invalidates the row math the next ToolEnd would use.
					run = nil
					footer.Refresh()
				}
			}
		}
	}()

	return resetSpawn
}
