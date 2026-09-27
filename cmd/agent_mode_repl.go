//go:build !js

package cmd

// agent_mode_repl.go — the interactive REPL loop, split out of
// runInteractiveMode (agent_mode_interactive.go). runInteractiveREPL
// runs until ctx is cancelled, the user exits (double Ctrl+C,
// exit/quit, or EOF), and handles per-turn steer/drain/summary work.
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

// interactiveREPLState carries the per-session REPL state that the loop
// mutates or inspects. autoQueued holds deferred queue messages that
// auto-submit as their own turns; pending holds carry-over text between
// turns (unsent steer text); lastInterruptAt tracks the last Ctrl+C so a
// second press within 2s exits (psql/redis-cli convention).
type interactiveREPLState struct {
	inputReader        *console.InputReader
	steerCoord         *SteerCoordinator
	footer             *console.StatusFooter
	autoQueued         []string
	lastInterruptAt    time.Time
	pending            PendingInput
	resetSpawnTracking func()
}

// runInteractiveREPL runs the interactive REPL loop. Split out of
// runInteractiveMode (agent_mode_interactive.go).
func runInteractiveREPL(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator, st interactiveREPLState) error {
	autoQueued := st.autoQueued
	lastInterruptAt := st.lastInterruptAt
	pending := st.pending
	inputReader := st.inputReader
	steerCoord := st.steerCoord
	footer := st.footer
	resetSpawnTracking := st.resetSpawnTracking

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var query string
			var rawQuery string

			// Check for auto-queued messages before blocking on ReadLine.
			if len(autoQueued) > 0 {
				query = autoQueued[0]
				autoQueued = autoQueued[1:]
				fmt.Fprintln(os.Stderr)
				if strings.HasPrefix(query, agent.WakeupBatchPrefix) {
					console.GlyphPaused.Fprintf(os.Stderr, "auto-resume: background task completed — resuming")
				} else {
					console.GlyphPaused.Fprintf(os.Stderr, "auto-run queued: %s", query)
				}
				rawQuery = ""
			} else {
				// SP-048-5d follow-up: refresh the prompt prefix each loop so
				// it tracks model changes (e.g. an LLM-driven /model switch
				// from inside a previous turn, or interactive provider/model
				// selection during recovery).
				inputReader.SetPrompt(cliui.BuildPromptPrefix(chatAgent.GetModel()))

				var err error
				query, err = inputReader.ReadLine()

				if err != nil {
					if errors.Is(err, console.ErrWakeupPending) {
						// A background event (auto-resume wakeup) needs the
						// REPL's turn machinery. Preserve any typed text and
						// loop; the top of the next iteration drains the
						// stashed wakeup batches into autoQueued.
						if strings.TrimSpace(inputReader.LineBuffer()) != "" {
							inputReader.SetInitialContent(inputReader.LineBuffer())
						}
						autoQueued = append(autoQueued, chatAgent.DrainWakeupForREPL()...)
						continue
					}
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
				rawQuery = query
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
			chatAgent.SetSlashCommands(registry)
			if registry.IsSlashCommand(query) {
				if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
					if !isReported(err) {
						fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
					}
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
			// Auto-submitted queued turns have rawQuery="" so they are
			// also excluded — only the user's actual typed input is
			// persisted.
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
			turnTotalStart := chatAgent.GetTotalTokens()
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
						if !isReported(err) {
							fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
						}
					}
				}()
			} // end if !fastPathExecuted

			// Drain all pending carry-over text (unsent steer buffer +
			// deferred queue messages) in a single call. Unsent text
			// becomes initial content for the next prompt; queued
			// messages are appended to autoQueued for auto-submit.
			pending = steerCoord.DrainPendingInput()
			if pending.InitialContent != "" {
				inputReader.SetInitialContent(pending.InitialContent)
			}
			autoQueued = append(autoQueued, pending.QueuedMessages...)
			// Refresh the footer so the "⏸ N queued" badge clears.
			footer.Refresh()
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
			// Charge REPL-run auto-resume turns against the wakeup budget —
			// the background-goroutine path does this in TryAutoResume, so
			// this keeps both surfaces equivalent.
			if strings.HasPrefix(query, agent.WakeupBatchPrefix) {
				if delta := chatAgent.GetTotalTokens() - turnTotalStart; delta > 0 {
					chatAgent.RecordWakeupTokens(delta, chatAgent.GetConfig().Wakeup)
				}
			}
		}
	}
}
