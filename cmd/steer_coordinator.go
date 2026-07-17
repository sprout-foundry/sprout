//go:build !js

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SteerCoordinator owns the lifecycle of the pinned steer-input panel
// across an interactive session (SP-055). It wires the
// SteerInputReader's submit and interrupt callbacks to the agent's
// InjectInputContext / TriggerInterrupt once, and toggles the reader
// on/off around each ProcessQuery call via StartTurn / EndTurn.
//
// Lifecycle:
//
//	c := NewSteerCoordinator(chatAgent, footer)
//	for {
//	    query := inputReader.ReadLine()
//	    c.StartTurn()
//	    ProcessQuery(...)
//	    c.EndTurn()
//	}
//
// Non-TTY runs construct a coordinator whose reader is a no-op, so
// callers don't need to gate the calls.
//
// Future polish (SP-055 Phase 3) hooks here: mode-indicator glyphs,
// steer history recall, "done queue" mode. Keeping the coordinator
// behind a single small surface (StartTurn / EndTurn) means those
// features can land without touching the REPL loop.
type SteerCoordinator struct {
	agent  *agent.Agent
	reader *console.SteerInputReader
	footer *console.StatusFooter
}

// NewSteerCoordinator constructs the coordinator with the SteerInputReader's
// callbacks already bound to the agent. The reader is created once and
// reused for every turn; SteerInputReader.Start/Stop reset its internal
// buffer between cycles.
//
// chatAgent and footer may be nil for tests; in that case StartTurn and
// EndTurn are no-ops.
func NewSteerCoordinator(chatAgent *agent.Agent, footer *console.StatusFooter) *SteerCoordinator {
	c := &SteerCoordinator{agent: chatAgent, footer: footer}
	if chatAgent == nil || footer == nil {
		return c
	}
	c.reader = console.NewSteerInputReader(
		footer,
		c.handleSteerSubmit,
		c.handleQueueSubmit,
		c.handleSteerInterrupt,
	)
	return c
}

// StartTurn activates the steer reader for the duration of a
// ProcessQuery call. Safe to call when the reader is already active
// (idempotent, the reader's own Start enforces this).
//
// Also registers the pause/resume hooks so interactive prompts (e.g.
// security elevation in pkg/utils.AskForConfirmation) can hand stdin
// back to cooked mode without fighting the steer reader for bytes.
// Without this hook the prompt's bufio.Reader hits EOF immediately
// and auto-rejects with "stdin unavailable - rejecting for safety".
func (c *SteerCoordinator) StartTurn() {
	if c == nil || c.reader == nil {
		return
	}
	// Clear the buffer at the start of each fresh turn. This was moved
	// here from Start() so that PauseSteer/ResumeSteer cycles (used by
	// interactive prompts mid-turn) preserve the in-progress text.
	c.reader.ResetBuffer()
	c.reader.Start()
	clihooks.SetSteerHooks(c.reader.Stop, c.reader.Start)
}

// EndTurn deactivates the steer reader and tears down the pinned line.
// Safe to call when already stopped.
func (c *SteerCoordinator) EndTurn() {
	if c == nil || c.reader == nil {
		return
	}
	clihooks.SetSteerHooks(nil, nil)
	c.reader.Stop()
}

// PendingInput captures all text that should carry over from one turn
// to the next: unsent steer text (typed but not submitted) and queued
// messages (submitted via Tab+Enter QUEUE mode). The REPL loop drains
// both in a single call to DrainPendingInput after EndTurn, eliminating
// the two-channel confusion where unsent text and queued messages
// followed different code paths.
type PendingInput struct {
	// InitialContent is text to pre-fill in the next prompt (unsent
	// steer text the user may want to edit before submitting).
	InitialContent string

	// QueuedPrefix is the formatted block of deferred messages to
	// prepend to the user's next submitted query. Empty when no
	// messages are queued.
	QueuedPrefix string

	// QueuedCount is how many deferred messages were drained (for
	// footer badge clearing and logging).
	QueuedCount int
}

// DrainPendingInput consolidates the two carry-over paths (unsent steer
// buffer + deferred queue messages) into a single drain. The REPL loop
// calls this once after EndTurn instead of separately calling
// DrainUnsentBuffer and DrainDeferredMessages.
//
// When both paths have content, the unsent text becomes the initial
// content (pre-filled for editing) and the queued messages become the
// prefix. This is the correct priority: the user was actively composing
// the unsent text, so it goes into the editable buffer; the queued
// messages are context they already decided on, so they prepend
// silently as before.
func (c *SteerCoordinator) DrainPendingInput() PendingInput {
	var pi PendingInput

	// 1. Drain unsent steer text → initial content for the next prompt.
	if c != nil && c.reader != nil {
		pi.InitialContent = c.reader.DrainUnsentBuffer()
		c.reader.ResetBuffer()
	}

	// 2. Drain deferred queue messages → formatted prefix.
	if c != nil && c.agent != nil {
		queued := c.agent.DrainDeferredMessages()
		pi.QueuedCount = len(queued)
		if len(queued) > 0 {
			var b strings.Builder
			b.WriteString("Queued from prior turn:\n")
			for _, msg := range queued {
				b.WriteString("  • ")
				b.WriteString(msg)
				b.WriteByte('\n')
			}
			pi.QueuedPrefix = strings.TrimSpace(b.String())
		}
	}

	return pi
}

// SetGroundTruth installs the REPL's pristine termios snapshot into
// the steer reader so Stop() restores to a known-good state instead
// of a potentially-corrupted per-enter snapshot.
func (c *SteerCoordinator) SetGroundTruth(gt *console.GroundTruthTermios) {
	if c == nil || c.reader == nil {
		return
	}
	c.reader.SetGroundTruth(gt)
}

// SetCompleter installs a slash-command completion provider on the
// steer reader (SP-078 Phase 2). Bound to Ctrl-] — Tab is reserved
// for the STEER ↔ QUEUE mode toggle. The same provider can be passed
// to both inputReader.SetCompleter (Tab, REPL prompt) and
// steerCoord.SetCompleter (Ctrl-], mid-turn) so completion works in
// both surfaces.
func (c *SteerCoordinator) SetCompleter(p console.CompletionProvider) {
	if c == nil || c.reader == nil {
		return
	}
	c.reader.SetCompleter(p)
}

// handleSteerSubmit forwards the user's typed message to the agent's
// inputInjectionChan (which the seed-integration bridge then routes
// into seed's InjectInput). On success an acknowledgement is printed
// to stderr in the scroll region so the user sees what they sent;
// failure (channel full) is reported similarly without breaking the
// turn.
//
// Timing reality: seed only consults its inputInjectionChan when the
// model returns a response WITH NO tool calls (seed v1.1.0
// conversation.go:476, gated by `len(assistantMsg.ToolCalls) == 0`).
// During a tool-execution loop the injection sits buffered until the
// model decides to stop. The ack therefore says "queued" rather than
// implying instant takeover. Users who want guaranteed next-turn
// behavior should toggle to QUEUE mode (Tab) which routes through
// the deferred queue instead.
func (c *SteerCoordinator) handleSteerSubmit(text string) {
	if c.agent == nil {
		return
	}
	if intent := ClassifyPromptIntent(c.agent, text); intent != IntentNone {
		if intent == IntentSlash {
			// Try to execute safe commands mid-turn
			if c.executeSteerCommand(text) {
				return
			}
		}
		rejectCommandIntent(intent, text, "steer", "wait for the prompt to finish (Ctrl+C / Esc to interrupt now)")
		return
	}
	// If a subagent is currently running, route the steer via
	// InjectInputIntoActive. SP-094-8: this now prefers the primary
	// agent first (which reads steer messages and decides whether to
	// abort subagents, redirect them, or fold the steer into its own
	// plan). Only if the primary's channel is full does it fall back
	// to the deepest running subagent.
	if runner := c.agent.GetSubagentRunner(); runner != nil {
		if target, ok := runner.InjectInputIntoActive(text); ok {
			fmt.Fprintln(os.Stderr)
			if target == "primary" {
				console.GlyphAction.Fprintf(os.Stderr, "steer queued: %s", text)
			} else {
				console.GlyphAction.Fprintf(os.Stderr, "steer → subagent (%s): %s", target, text)
			}
			return
		}
	}
	if err := c.agent.InjectInputContext(text); err != nil {
		fmt.Fprintln(os.Stderr)
		console.GlyphError.Fprintf(os.Stderr, "steer dropped: %v", err)
		return
	}
	fmt.Fprintln(os.Stderr)
	console.GlyphAction.Fprintf(os.Stderr, "steer queued: %s", text)
}

// handleSteerInterrupt routes Ctrl+C-while-steering to the same
// TriggerInterrupt that the SIGINT handler uses in cooked mode. The
// two paths intentionally converge: whichever surface the user reaches
// for to stop a turn, the underlying mechanism is the same.
//
// The string argument is unused — it exists for API symmetry with
// SteerInputReader.NewSteerInputReader, which uses a single closure
// shape for both callbacks.
func (c *SteerCoordinator) handleSteerInterrupt(_ string) {
	if c.agent == nil {
		return
	}
	c.agent.TriggerInterrupt()
}

// handleQueueSubmit is the QUEUE-mode counterpart to handleSteerSubmit.
// The message is enqueued on the agent's deferred queue and will be
// joined with the user's next typed prompt when the REPL drains it
// (SP-055 Phase 3b). Mid-turn streaming is unaffected — nothing is
// injected into the active turn.
func (c *SteerCoordinator) handleQueueSubmit(text string) {
	if c.agent == nil || text == "" {
		return
	}
	if intent := ClassifyPromptIntent(c.agent, text); intent != IntentNone {
		// Queue mode wraps drained messages into a "Queued from prior
		// turn:" blockquote at the next prompt, which strips the leading
		// '/' or '!' and the prompt's IsSlashCommand / fast-path checks
		// stop matching. Rather than silently demoting the command to
		// LLM text, reject and tell the user where to send it.
		rejectCommandIntent(intent, text, "queue", "type it at the prompt after this turn ends")
		return
	}
	c.agent.EnqueueDeferredMessage(text)
	fmt.Fprintln(os.Stderr)
	console.GlyphPaused.Fprintf(os.Stderr, "queued: %s", text)
	// Refresh the footer so the new "⏸ N queued" badge appears in the
	// same frame the user submitted. Without this nudge the badge
	// would lag until the next tool/cost event fires.
	if c.footer != nil {
		c.footer.Refresh()
	}
}

// rejectCommandIntent prints a single-line stderr warning explaining
// why a steer / queue submission was dropped. mode is "steer" or
// "queue"; remedy is the actionable hint the user can take next.
// The dropped text is echoed verbatim (truncated) so the user can see
// what they almost sent.
func rejectCommandIntent(intent PromptIntent, text, mode, remedy string) {
	preview := strings.TrimSpace(text)
	const maxPreview = 60
	if len(preview) > maxPreview {
		preview = preview[:maxPreview-1] + "…"
	}
	fmt.Fprintln(os.Stderr)
	console.GlyphWarning.Fprintf(os.Stderr,
		"%s mode can't run a %s — %s. Dropped: %s",
		mode, string(intent), remedy, preview,
	)
}

// executeSteerCommand tries to run a slash command mid-turn. Returns true
// if the command was handled (safe and executed, or unsafe and rejected).
func (c *SteerCoordinator) executeSteerCommand(text string) bool {
	parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(text), "/"))
	if len(parts) == 0 {
		return false
	}
	cmdName := parts[0]

	registryRaw := c.agent.SlashCommands()
	if registryRaw == nil {
		return false
	}
	registry, ok := registryRaw.(*agent_commands.CommandRegistry)
	if !ok {
		return false
	}

	cmd, ok := registry.GetCommand(cmdName)
	if !ok {
		return false
	}

	sc, ok := cmd.(agent_commands.SteerCapable)
	if !ok || !sc.SafeDuringSteer() {
		return false // will fall through to rejectCommandIntent
	}
	// Execute in a goroutine to avoid blocking the steer reader goroutine.
	// The command writes to stdout/stderr which is fine — the terminal subscriber
	// will pick it up. Use recover to prevent a command panic from killing the
	// steer goroutine.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				console.GlyphError.Fprintf(os.Stderr, "command /%s panicked: %v", cmdName, r)
			}
		}()
		if err := cmd.Execute(parts[1:], c.agent); err != nil {
			console.GlyphError.Fprintf(os.Stderr, "command /%s: %v", cmdName, err)
		}
	}()
	return true
}
