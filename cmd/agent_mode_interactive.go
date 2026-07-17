//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// runInteractiveMode handles interactive REPL mode
func runInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) error {
	// SP-048 follow-up: Go's default logger writes to stderr — and so does
	// the activity-indicator spinner. Without redirection, any log.Printf
	// fired during a tool run (e.g. the [WARN] in pkg/configuration/config.go
	// when an AllowedTools override is dropped) interleaves with spinner
	// frames and produces the cursor-thrash bug we caught in real sessions.
	// Route Go's log to .sprout/workspace.log instead so internal noise
	// stops fighting the spinner; user-facing output still goes through
	// fmt.Print which is properly synchronized by the indicator.
	if restoreLog, err := redirectGoLogToWorkspace(); err == nil {
		defer restoreLog()
	}

	// SP-048-3: Persistent status footer pinned at the bottom row of the
	// terminal. Suppressed automatically on non-TTY (e.g., piped output).
	// MUST be Stopped before exit or the user's terminal is left with a
	// broken scroll region — both the defer here AND the signal handler's
	// force-quit path call Stop via the global registration.
	//
	// Started BEFORE the welcome/recent-sessions prints so that intro
	// output lands inside the scroll region (1..N-2) and scrolls naturally
	// as the session grows. Reverse order (prints first, then footer)
	// leaves the cursor inside already-printed content at row N-2, and
	// the input prompt then renders on top of it.
	footerSource := &agentFooterSource{agent: chatAgent}
	footer := console.NewStatusFooter(os.Stderr, footerSource)
	console.RegisterGlobalStatusFooter(footer)
	footer.Start()
	defer footer.Stop()

	// CLI-UX-12: register Alt+T (footer tooltip toggle) and Alt+V
	// (output verbosity toggle) in the global keymap so power users
	// can switch verbosity live without /settings + restart. The
	// verbosity toggle reads cfg.OutputVerbosity on each press; the
	// terminal subscriber's isVerbose()/isCompact() helpers pick up
	// the change on the next tool event (live-read). Idempotent — the
	// registry uses sync.Once, so multiple mode-bootstrap calls
	// (interactive + queue) only register once.
	console.RegisterKeymapForFooter(footer, chatAgent.GetConfigManager())

	// Compact startup chrome: a single greeting line with the active
	// provider/model so the first impression is "who am I talking to"
	// rather than four rows of welcome / provider / model / blank.
	// The previous form printed "Welcome to sprout! Enhanced CLI with
	// Web UI" + a second "Provider: X | Model: Y" line with trailing
	// blanks — five rows before the user could type anything.
	fmt.Println()
	console.GlyphInfo.Printf("sprout · %s · %s",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// SP-048-5a: surface recent sessions (last 7d) with inline numeric
	// selection. Up/down arrows stay reserved for command history; a
	// fresh number on its own line is the affordance. If the user picks
	// a session, this loads its state in-place via LoadStateScoped +
	// ApplyState + SetSessionID — same mechanism as `--session-id`,
	// just triggered interactively.
	//
	// Runs BEFORE the InputReader is constructed so a resumed session's
	// model is reflected in the prompt prefix. The returned dismissKey
	// is the first character the user typed to dismiss the picker (if
	// any) — it's forwarded into the input buffer below so that
	// keystroke isn't swallowed.
	dismissKey := maybeOfferSessionResume(chatAgent)

	// SP-048-5b: one-shot hint about Tab autocomplete + Ctrl-D, persisted
	// per workspace in ~/.sprout/state.json so it never repeats.
	maybeShowFirstRunHint()

	// Embeddings are opt-in (they load a ~380MB model); recommend turning them
	// on once per workspace so the feature stays discoverable without nagging.
	maybeRecommendEmbeddingIndex(chatAgent)

	// Create enhanced input reader with completion support.
	// SP-048-5d: prompt includes the current model so users always know
	// what they're talking to. Falls back to "sprout> " when the model
	// name is empty (e.g. provider failed to resolve at startup).
	inputReader := console.NewInputReader(cliui.BuildPromptPrefix(chatAgent.GetModel()))

	// Initialize with existing history from agent
	inputReader.SetHistory(chatAgent.GetHistory())

	// Forward the picker's dismiss key into the REPL input buffer so the
	// first character the user typed to start fresh isn't swallowed by
	// the session picker. SetInitialContent pre-fills the buffer; the
	// next ReadLine renders it with the cursor at the end.
	if dismissKey != "" {
		inputReader.SetInitialContent(dismissKey)
	}

	// SP-048-2a: slash command tab completion. The registry is cached
	// per-session (see slashCommandCache); argument completions are
	// TTL-cached to avoid network/config reads on every keystroke.
	completer := buildSlashCommandCompleter(chatAgent)
	inputReader.SetCompleter(completer)
	inputReader.SetRichCompleter(buildRichSlashCommandCompleter(chatAgent))

	// SP-055: steer coordinator owns the pinned steer-input panel for
	// the lifetime of this REPL. Constructed once with the agent +
	// footer references; StartTurn / EndTurn drive the per-iteration
	// lifecycle below.
	steerCoord := NewSteerCoordinator(chatAgent, footer)

	// SP-078 Phase 2: same slash-command completer on the steer panel
	// so Ctrl-] cycles slash commands mid-turn (Tab is reserved for
	// STEER ↔ QUEUE mode toggle on the steer panel).
	steerCoord.SetCompleter(completer)

	// Capture a ground-truth termios snapshot of stdin in its default
	// cooked state (the terminal is fully cooked at this point — no
	// raw or steer mode active). Both InputReader and SteerInputReader
	// use this for emergency recovery: if a prior mode transition leaves
	// the terminal in raw mode, the pre-flight check restores to this
	// known-good state instead of a potentially-corrupted per-enter
	// snapshot. Must be captured AFTER footer.Start() so the scroll
	// region is established, but BEFORE any ReadLine / StartTurn call.
	groundTruth := console.CaptureGroundTruth()
	inputReader.SetGroundTruth(groundTruth)
	steerCoord.SetGroundTruth(groundTruth)

	// SP-048-1c + 3: Subscribe to tool start/end events so the activity
	// indicator can render a per-tool timeline AND the footer can refresh
	// cost/context after each tool. Runs until ctx is cancelled.
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	resetSpawnTracking := cliui.StartTerminalToolSubscriber(subCtx, chatAgent, eventBus, indicator, footer)

	// Tracks the time of the last Ctrl+C at the idle prompt so a
	// second press within 2s exits the REPL (standard convention:
	// psql, redis-cli, node). Reset to zero on any successful read.
	var lastInterruptAt time.Time

	// pending holds carry-over text between turns: unsent steer text
	// (→ SetInitialContent) and deferred queue messages (→ prepend to
	// next query). Drained once per turn via DrainPendingInput.
	var pending PendingInput

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// SP-048-5d follow-up: refresh the prompt prefix each loop so
			// it tracks model changes (e.g. an LLM-driven /model switch
			// from inside a previous turn, or interactive provider/model
			// selection during recovery).
			inputReader.SetPrompt(cliui.BuildPromptPrefix(chatAgent.GetModel()))

			query, err := inputReader.ReadLine()

			if err != nil {
				if err.Error() == "interrupted" {
					// Standard REPL convention (psql, redis-cli, node):
					// first Ctrl+C at an empty prompt clears the line and
					// shows a brief hint; a second Ctrl+C within a short
					// window exits. The input reader has already cleared
					// and re-rendered the prompt line; we just track the
					// timing to detect the double-press.
					now := time.Now()
					if now.Sub(lastInterruptAt) < 2*time.Second {
						fmt.Println()
						console.GlyphInfo.Printf("Goodbye!")
						printContinuationHint(chatAgent)
						return nil
					}
					lastInterruptAt = now
					fmt.Println("(press Ctrl+C again to exit)")
					continue
				}
				// EOF and context cancellation are graceful exits, not
				// errors. When the web server shuts down or the context
				// is cancelled, ReadLine returns io.EOF — treating it as
				// an error prints "✗ failed to run agent: EOF" on exit,
				// which looks like a crash.
				if err == io.EOF || errors.Is(err, context.Canceled) || errors.Is(err, io.ErrClosedPipe) {
					return nil
				}
				return fmt.Errorf("failed to read input: %w", err)
			}
			// A successful read resets the double-Ctrl+C window so the
			// next interrupt cycle starts fresh.
			lastInterruptAt = time.Time{}

			query = strings.TrimSpace(query)
			rawQuery := query // user's typed text, before deferred-message prepend

			// Prepend deferred queue messages (drained at the end of the
			// previous turn via DrainPendingInput). The queued prefix is
			// stored on the steer coordinator so the REPL loop has a
			// single source of truth for carry-over text.
			if pending.QueuedPrefix != "" {
				if query == "" {
					query = pending.QueuedPrefix
				} else {
					query = pending.QueuedPrefix + "\n" + query
				}
				pending.QueuedPrefix = "" // consumed
				// Refresh the footer so the "⏸ N queued" badge clears.
				footer.Refresh()
			}
			if query == "" {
				continue
			}

			// Handle exit commands (before history — don't persist these)
			if strings.ToLower(query) == "exit" || strings.ToLower(query) == "quit" {
				fmt.Println("\n-- Goodbye! Here's your session summary:")
				fmt.Println("=====================================")
				chatAgent.PrintConversationSummary(true)
				printContinuationHint(chatAgent)
				return nil
			}

			// `?` shortcut: print a compact keyboard-help card and
			// return to the prompt without consuming an LLM turn. Helps
			// users discover the steer-panel keys (Tab toggle, ↑↓
			// history) that aren't advertised elsewhere.
			if query == "?" {
				printKeyboardHelp()
				continue
			}

			// Slash/bang commands run locally — they don't talk to the LLM
			// and often own the terminal themselves (interactive `/commit`,
			// `/persona`, etc.). They MUST NOT have the activity-indicator
			// spinner active during execution: the spinner's stderr writes
			// would interleave with the command's own stdout prompts and
			// produce the input-mangling bug we caught in `/commit` and
			// friends. Slash commands also skip the per-turn cost summary
			// since they don't consume LLM tokens.
			registry := agent_commands.NewCommandRegistry()
			if registry.IsSlashCommand(query) {
				if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
					fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
				}
				// `/model` and friends may have changed the active model;
				// rebuild the prompt prefix so the next prompt reflects it.
				inputReader.SetPrompt(cliui.BuildPromptPrefix(chatAgent.GetModel()))
				footer.Refresh()
				continue
			}

			// Add to agent history — only genuine LLM-bound prompts
			// are persisted. `?`, exit/quit, and slash commands are
			// intentionally excluded so they don't pollute ↑/Ctrl-R.
			// We persist rawQuery (the user's typed text) rather than
			// the composite `query`, so recalling a deferred-message
			// turn via ↑ doesn't replay the "Queued from prior turn:"
			// template — only the user's actual input.
			if rawQuery != "" {
				chatAgent.AddToHistory(rawQuery)
				inputReader.SetHistory(chatAgent.GetHistory())
			}

			// SP-048-5c: snapshot per-turn metrics before submit so we can
			// emit a "this turn" cost / tokens / elapsed line after the
			// model finishes.
			turnStart := time.Now()
			turnPromptStart := chatAgent.GetPromptTokens()
			turnCompletionStart := chatAgent.GetCompletionTokens()
			// Clear the ttft tracker so the next stream chunk sets a
			// fresh "time to first token" measurement for this turn.
			cliui.ResetTurnFirstToken()

			// SP-051-2c: clear per-turn spawn dedupe so the next batch of
			// subagents announces fresh "↳ persona spawned" lines instead of
			// silently joining whatever ran in the prior turn.
			resetSpawnTracking()

			// Role header so the boundary between user input and assistant
			// reply is visually obvious. Uses a brand-colored bar + dim
			// "assistant" label — pops out in scrollback without being noisy.
			// Paired at the bottom with the existing dim `⎯ this turn: … ⎯`
			// summary line, which acts as the closing separator.
			fmt.Println()
			cliui.PrintAssistantHeader(chatAgent.GetModel())

			// Per-turn assistant renderer: indents prose with "  " as it
			// streams, and at turn-end optionally re-renders the final
			// prose segment with markdown formatting (cursor-clear +
			// reprint). Wire OnExternalWrite into the OutputRouter so
			// tool-log lines break the current prose segment cleanly.
			turnRenderer := beginTurn(chatAgent)
			if router := chatAgent.OutputRouter(); router != nil {
				// SP-056: When reasoning mode is "fold", route reasoning chunks to
				// the fold instead of the turn renderer's collapsed header.
				if fold := currentReasoningFold; fold != nil {
					fold.Start()
					router.SetReasoningCallback(fold.Chunk)
				} else {
					// SP-061: route reasoning chunks to the renderer's
					// dedicated sink so they collapse into a single
					// "▽ Thinking · N kB" header rather than streaming
					// raw monologue. Only takes effect when
					// SetReasoningTerminalEnabled(true) — by default the
					// CLI still suppresses reasoning entirely.
					router.SetReasoningCallback(turnRenderer.WriteReasoningChunk)
				}
			}

			// SP-048-1b: Try fast paths BEFORE starting the "Thinking"
			// spinner so the user never sees the LLM spinner for commands
			// that execute directly without LLM involvement.
			var fastPathExecuted bool
			// Try zsh command detection first (fast path)
			if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
				fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
			} else if executed {
				fastPathExecuted = true
			}

			// Only start the spinner (and the full agent turn) when no fast
			// path handled the query.
			if !fastPathExecuted {
				indicator.Start(fmt.Sprintf("Thinking · %s", chatAgent.GetModel()))

				// Execute the turn inside a func so we can defer EndTurn.
				// This ensures the steer reader is always stopped even if
				// ProcessQuery panics, preventing the terminal from being
				// left in raw/cbreak mode.
				func() {
					// SP-055: turn the steer panel on for the duration of the
					// ProcessQuery call. The coordinator (constructed once at
					// session start) owns the SteerInputReader and the callback
					// wiring to InjectInputContext / TriggerInterrupt.
					steerCoord.StartTurn()
					defer steerCoord.EndTurn()

					// No fast path triggered, process normally via LLM
					if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
						indicator.Stop()
						fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
					}
				}()
			} // end if !fastPathExecuted

			// Drain all pending carry-over text (unsent steer buffer +
			// deferred queue messages) in a single call. Unsent text
			// becomes initial content for the next prompt; queued
			// messages become a prefix that prepends to the next
			// submitted query.
			pending = steerCoord.DrainPendingInput()
			if pending.InitialContent != "" {
				inputReader.SetInitialContent(pending.InitialContent)
			}
			// Defensive: ensure the spinner is cleared at the end of every turn
			// even if the streamFn never fired (e.g. zsh fast-path executed).
			indicator.Stop()
			// Finalize the assistant renderer: re-renders the final prose
			// segment with markdown formatting when it's substantial
			// enough to be worth the cursor-clear flicker. Tear down the
			// external-write hook BEFORE FinalizeAtTurnEnd so the
			// re-render's own writes don't loop back through it.
			if router := chatAgent.OutputRouter(); router != nil {
				// SP-056: Resolve any active fold at turn end (catches the case
				// where reasoning ended but no assistant text arrived).
				if fold := currentReasoningFold; fold != nil && fold.IsActive() {
					fold.Resolve()
				}
			}
			endTurn(chatAgent, turnRenderer)
			// SP-070-2: notify the user when a long turn completes
			cliui.NotifyTurnCompletion(chatAgent, turnStart, agentSkipPrompt)
			// SP-048-3: refresh the footer at turn-end so cost / context /
			// model changes (e.g. /model switch) land immediately.
			footer.Refresh()
			// SP-048-5c: print the per-turn summary line if any LLM tokens
			// were actually consumed. Suppressed for zero-cost turns (slash
			// commands, zsh fast paths, empty responses).
			cliui.PrintPerTurnSummary(chatAgent, turnStart, turnPromptStart, turnCompletionStart)
		}
	}
}

// agentFooterSource adapts *agent.Agent to the console.ContentSource
// interface, exposing model / context tokens / cost / cwd to the status
// footer renderer.
type agentFooterSource struct {
	agent *agent.Agent
}

func (s *agentFooterSource) Model() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return cliui.ShortModelName(s.agent.GetModel())
}

func (s *agentFooterSource) ContextTokens() (used, limit int) {
	if s == nil || s.agent == nil {
		return 0, 0
	}
	return s.agent.GetContextTokens()
}

func (s *agentFooterSource) TotalCost() float64 {
	if s == nil || s.agent == nil {
		return 0
	}
	return s.agent.GetTotalCost()
}

// BillingType returns the current provider's billing model so the footer
// can annotate subscription/free usage instead of showing "$0.0000".
// Satisfies the optional billingTypeSource interface in pkg/console.
// SP-113 Phase 3.
func (s *agentFooterSource) BillingType() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return s.agent.ResolveBillingType()
}

// TodoProgress returns (completed, total) from the agent's todo list.
// Satisfies the optional todoProgressSource interface so the footer can
// render a "3/7 done" badge during multi-step turns. CLI-UX-4.
func (s *agentFooterSource) TodoProgress() (done, total int) {
	if s == nil || s.agent == nil {
		return 0, 0
	}
	todos := s.agent.GetTodoManager().Read()
	for _, t := range todos {
		total++
		if t.Status == "completed" {
			done++
		}
	}
	return done, total
}

func (s *agentFooterSource) WorkingDir() string {
	wd, _ := os.Getwd()
	return wd
}

// ActiveSubagents satisfies the optional activeSubagentsSource interface in
// pkg/console so the footer can render " · N sub" while subagents are
// in flight. SP-051-2d.
func (s *agentFooterSource) ActiveSubagents() int {
	return agent.GetActiveSubagents()
}

// QueuedMessages satisfies the optional queuedMessagesSource interface
// so the footer renders a "⏸ N queued" badge when the user has
// deferred steer messages via Tab+Enter waiting for the next turn.
// SP-055 Phase 3b.
func (s *agentFooterSource) QueuedMessages() int {
	if s.agent == nil {
		return 0
	}
	return s.agent.DeferredMessageCount()
}
