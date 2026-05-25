//go:build !js

package cmd

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/agent"
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
func (c *SteerCoordinator) StartTurn() {
	if c == nil || c.reader == nil {
		return
	}
	c.reader.Start()
}

// EndTurn deactivates the steer reader and tears down the pinned line.
// Safe to call when already stopped.
func (c *SteerCoordinator) EndTurn() {
	if c == nil || c.reader == nil {
		return
	}
	c.reader.Stop()
}

// handleSteerSubmit forwards the user's typed message to the agent's
// inputInjectionChan (which the seed-integration bridge then routes
// into seed's InjectInput). On success an acknowledgement is printed
// to stderr in the scroll region so the user sees what they sent;
// failure (channel full) is reported similarly without breaking the
// turn.
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
	console.GlyphAction.Fprintf(os.Stderr, "steer: %s", text)
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
