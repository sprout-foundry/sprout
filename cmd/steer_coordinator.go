//go:build !js

package cmd

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/agent"
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

// DrainUnsentBuffer returns any text the user typed into the steer
// panel during the last turn but did not submit. The REPL loop calls
// this after EndTurn and carries the text into the next ReadLine via
// InputReader.SetInitialContent. The steer buffer is reset after drain.
func (c *SteerCoordinator) DrainUnsentBuffer() string {
	if c == nil || c.reader == nil {
		return ""
	}
	text := c.reader.DrainUnsentBuffer()
	c.reader.ResetBuffer()
	return text
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
