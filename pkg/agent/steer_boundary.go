package agent

import (
	"context"
	"sync"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// seedInjector is the slice of seed's Agent surface the boundary deliverer
// needs. *core.Agent satisfies it.
type seedInjector interface {
	InjectInput(input string) bool
}

// seedStateReader is the optional half of seed's surface the deliverer uses to
// check whether an accepted steer actually reached the conversation.
// *core.Agent satisfies it; a test double that does not is treated as
// unobservable, so its accepted steer is released rather than silently kept.
type seedStateReader interface {
	State() *core.State
}

// The landing proof reads the live conversation off the seed agent, so the
// concrete type the deliverer is wired with must expose it. If seed ever drops
// this, landedObserver would silently start returning nil and every accepted
// steer would be treated as stranded.
var _ seedStateReader = (*core.Agent)(nil)

// steerCopies counts how many times stamped — the exact bytes handed to
// InjectInput — appears in the conversation as a user message, so a copy in the
// state is proof the message left the injection buffer and entered the
// pipeline.
//
// This relies on seed inserting injected input verbatim: both pickup points do
// AddMessage(Message{Role: "user", Content: injectedInput}) with no rewriting,
// which is why an exact byte comparison (envelope and all) identifies the
// message. If seed ever normalises or re-stamps injected text, compare a
// normalised form here instead — an unmatchable proof makes every landed steer
// look stranded.
func steerCopies(st *core.State, stamped string) int {
	if st == nil || stamped == "" {
		return 0
	}
	n := 0
	for _, m := range st.Messages() {
		if m.Role == "user" && m.Content == stamped {
			n++
		}
	}
	return n
}

// landedObserver counts conversation copies for a given seed agent, or nil when
// there is nothing to observe. A nil observer means landing cannot be proven,
// so an accepted message is released for redelivery rather than kept.
func landedObserver(sa seedInjector) func(string) int {
	if sa == nil {
		return nil
	}
	if ca, ok := sa.(*core.Agent); ok && ca == nil {
		return nil
	}
	sr, ok := sa.(seedStateReader)
	if !ok {
		return nil
	}
	return func(stamped string) int { return steerCopies(sr.State(), stamped) }
}

// steerBoundaryDeliverer hands one staged steer message to seed per call.
// It is invoked at conversation-loop boundaries by sprout-owned code that
// runs inside seed's loop goroutine (provider return, tool-batch return),
// immediately before seed's own injection pickup checks — so delivery
// timing is identical to the old eager-channel pipeline.
//
// A single non-blocking InjectInput attempt per boundary is intentional:
// seed consumes at most one injected message per boundary and its channel
// holds one; a rejected attempt means the previous message is still being
// processed, and the staged message simply waits for the next boundary.
type steerBoundaryDeliverer struct {
	mu        sync.Mutex
	agent     *Agent
	seedAgent seedInjector
}

func (d *steerBoundaryDeliverer) setSeedAgent(sa seedInjector) {
	d.mu.Lock()
	d.seedAgent = sa
	d.mu.Unlock()
}

// deliverOne offers the oldest pending steer to seed. Returns true when a
// message was accepted (and marked awaiting landing); false when nothing was
// pending or seed's buffer was busy.
func (d *steerBoundaryDeliverer) deliverOne() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	agent, sa := d.agent, d.seedAgent
	d.mu.Unlock()
	if agent == nil || sa == nil {
		return false
	}

	// Settle any earlier hand-off before offering another message: seed's
	// pickup points are the only places its injection buffer drains, so a
	// message whose text is now in the conversation is delivered and leaves the
	// list. One still unaccounted for at the turn's end is released for
	// redelivery (see releaseUnlandedSteer).
	agent.resolveLandedSteer(landedObserver(sa))

	id, content, ok := agent.peekStagedSteer()
	if !ok {
		return false
	}
	sent := content
	// Stamp the steer message with the active turn's timestamp as it
	// enters seed's state — same injection-time rule as the turn's query.
	// The staged copy stays clean so retraction returns the user's text,
	// and the stamped bytes become part of the conversation prefix that
	// later requests replay byte-identically.
	agent.turnTimestampMu.RLock()
	turnStamp := agent.turnTimestamp
	agent.turnTimestampMu.RUnlock()
	if !turnStamp.IsZero() {
		content = InjectUserMessageTimestampAt(content, turnStamp)
	}
	if !sa.InjectInput(content) {
		agent.releaseStagedSteer(id)
		return false
	}
	agent.markAcceptedSteer(id, content)
	agent.publishEvent(events.EventTypeSteerDelivered, map[string]interface{}{"content": sent})
	return true
}

// steerFlushExecutor wraps a core.ToolExecutor so the oldest staged steer
// is offered to seed after every tool batch — the moment seed's loop is
// about to run its post-tool injection check.
type steerFlushExecutor struct {
	inner     core.ToolExecutor
	deliverer *steerBoundaryDeliverer
}

func (s *steerFlushExecutor) GetTools() []core.Tool {
	return s.inner.GetTools()
}

func (s *steerFlushExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	results := s.inner.Execute(ctx, calls)
	s.deliverer.deliverOne()
	return results
}

// setProviderSteerHook installs a post-provider-call boundary hook on the
// sprout provider. Called at the END of Chat/ChatStream, covering seed's
// no-tool-call injection check (conversation.go:625 pickup point).
func setProviderSteerHook(p core.Provider, hook func()) {
	if sp, ok := p.(*sproutProvider); ok {
		sp.setSteerFlushHook(hook)
	}
}

// recoverUnlandedSteer settles accepted-but-unconfirmed steer messages at the
// end of a run. A run can exit between seed accepting a message into its
// one-slot per-run injection buffer and a pickup consuming it; that buffer is
// not part of the conversation and dies with the run's seed agent, so the
// user's steer would simply vanish. Whatever the conversation cannot vouch for
// goes back to the staging queue for a later turn to deliver.
//
// The run's error is deliberately not consulted. Seed's force-finalize exits
// return no error without running a pickup, so a nil error proves nothing about
// the buffer, and its termination reason is recorded by handleQueryResult after
// this point, still describing the previous turn.
func (a *Agent) recoverUnlandedSteer(counted func(string) int) {
	_ = a.releaseUnlandedSteer(counted)
}
