package agent

import (
	"sync"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// steerStageCap bounds the pending-steer queue. Matches inputInjectionBufferSize
// so staging rejects overflow at the same point the legacy channel did.
const steerStageCap = 10

// pendingSteer is one user-steer message waiting to be delivered into seed.
type pendingSteer struct {
	id      uint64 // monotonic per-agent; commit-by-id avoids retract races
	content string
	// inFlight marks the instant between handing the message to seed and
	// learning whether seed took it; not retractable.
	inFlight bool
	// accepted marks a message seed's InjectInput took into its per-run
	// injection buffer. That buffer holds one message and drains only at
	// seed's loop-boundary pickup points, so acceptance is NOT delivery: a run
	// can exit — interrupted, at max iterations, with an error, or through a
	// force-finalize that returns no error — before a pickup consumes it, and
	// the buffer dies with that turn's seed agent. An accepted entry keeps its
	// place in the list until the conversation proves it landed (then it is
	// removed) or the run ends without that proof (then it goes back to
	// pending for a later turn to deliver).
	accepted bool
	// stamped is the exact bytes handed to InjectInput, kept for the landing
	// check. content stays the user's own text so retraction returns it.
	stamped string
	// stranding counts how many times a run took this message and ended
	// without it landing. Each stranding queues a redelivery; a message
	// stranded again past steerStrandingCap is dropped rather than injected on
	// every subsequent turn.
	stranding int
}

// steerStrandingCap bounds how many runs may accept a steer message without it
// ever reaching the conversation, so a message whose landing can never be
// proven (its text is unfindable in state) cannot be re-injected on every turn.
// Past the cap the message is dropped and reported, never dropped silently.
const steerStrandingCap = 2

// steerStage is the retractable ordered queue that replaces the old
// channel-forwarder pattern for mid-turn steer delivery. Entries are
// delivered one per conversation-loop boundary (see deliverStagedToSeed)
// because seed consumes at most one injected message per boundary check
// and its injection channel holds exactly one — attempting to push more
// per boundary would either block the loop goroutine or drop messages.
type steerStage struct {
	mu      sync.Mutex
	entries []pendingSteer
	nextID  uint64
}

// getSteerStage lazily initialises the per-agent staging struct.
func (a *Agent) getSteerStage() *steerStage {
	a.steerStageMu.Lock()
	defer a.steerStageMu.Unlock()
	if a.steerStage == nil {
		a.steerStage = &steerStage{}
	}
	return a.steerStage
}

// StageSteerInput appends a steer message to the retractable pending list.
// Mirrors the text into inputInjectionChan (best-effort, non-blocking) so
// legacy consumers that read SteeringChannel directly keep observing
// submissions; nothing in the delivery path drains that channel anymore.
func (a *Agent) StageSteerInput(text string) error {
	if a == nil {
		return agenterrors.NewTransientError("failed to inject input: agent is nil", nil)
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if len(ss.entries) >= steerStageCap {
		return agenterrors.NewTransientError("failed to inject input: input injection channel is full", nil)
	}

	ss.entries = append(ss.entries, pendingSteer{
		id:      ss.nextID,
		content: text,
	})
	ss.nextID++

	if a.inputInjectionChan != nil {
		select {
		case a.inputInjectionChan <- text:
		default:
		}
	}
	return nil
}

// peekStagedSteer marks the oldest pending steer in-flight and returns it.
// Entries already accepted by seed (awaiting proof they landed) and entries
// mid-hand-off are both skipped: the accepted one is already in seed's
// buffer, and offering a second while that buffer is busy is rejected there.
// If seed rejects the message, releaseStagedSteer clears the flag so it stays
// retractable.
func (a *Agent) peekStagedSteer() (uint64, string, bool) {
	if a == nil {
		return 0, "", false
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := range ss.entries {
		if ss.entries[i].inFlight || ss.entries[i].accepted {
			continue
		}
		ss.entries[i].inFlight = true
		return ss.entries[i].id, ss.entries[i].content, true
	}
	return 0, "", false
}

// markAcceptedSteer records that seed took the message into its per-run
// injection buffer. The entry stays staged (position kept for FIFO) and is
// invisible to retraction until resolveLandedSteer removes it or
// releaseUnlandedSteer puts it back.
func (a *Agent) markAcceptedSteer(id uint64, stamped string) {
	if a == nil {
		return
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := range ss.entries {
		if ss.entries[i].id == id {
			ss.entries[i].inFlight = false
			ss.entries[i].accepted = true
			ss.entries[i].stamped = stamped
			return
		}
	}
}

// settleAcceptedSteer resolves every accepted entry against the conversation
// and drops the ones it can vouch for. counted reports how many copies of a
// stamped text are present; entries match by rank, so the k-th accepted copy of
// a text needs that text present at least k times. Two steers with identical
// text are stamped identically, and a bare presence test would let the first
// copy vouch for the second.
//
// release decides the fate of an entry the conversation cannot vouch for: false
// keeps it accepted for a later boundary or the run's end to settle; true puts
// it back on the pending queue, bounded by steerStrandingCap.
func (a *Agent) settleAcceptedSteer(counted func(stamped string) int, release bool) int {
	if a == nil {
		return 0
	}
	log := a.Logger()
	ss := a.getSteerStage()
	ss.mu.Lock()
	if !hasAcceptedSteer(ss.entries) {
		ss.mu.Unlock()
		return 0
	}

	ranks := map[string]int{}
	released := 0
	var lost []string
	kept := ss.entries[:0]
	for _, e := range ss.entries {
		if !e.accepted {
			kept = append(kept, e)
			continue
		}
		ranks[e.stamped]++
		if counted != nil && counted(e.stamped) >= ranks[e.stamped] {
			continue // vouched for: it is in the conversation
		}
		if !release {
			kept = append(kept, e)
			continue
		}
		e.stranding++
		if e.stranding > steerStrandingCap {
			lost = append(lost, e.content)
			continue
		}
		e.accepted = false
		e.stamped = ""
		kept = append(kept, e)
		released++
	}
	ss.entries = kept
	ss.mu.Unlock()

	if released > 0 {
		log.Debug("[steer] %d steer message(s) taken by a run that ended before delivering them are queued for redelivery\n", released)
	}
	for _, content := range lost {
		// A steer that keeps getting stranded is normally still sitting in the
		// user's input history, but dropping it silently would be the exact
		// failure this path exists to prevent, so say so.
		log.Warn("[steer] dropped a steer message that never reached the conversation: %s\n", content)
		a.publishEvent(events.EventTypeError, events.ErrorEvent(
			"Steer message was never delivered and has been dropped; send it again", nil))
	}
	return released
}

// resolveLandedSteer drops the accepted entries the conversation can vouch for,
// keeping the rest for a later boundary or the run's end to settle. Run at each
// conversation boundary, it frees the queue as soon as seed consumes an entry,
// so a later message can take the buffer.
func (a *Agent) resolveLandedSteer(counted func(stamped string) int) {
	_ = a.settleAcceptedSteer(counted, false)
}

// releaseUnlandedSteer returns accepted entries the conversation cannot vouch
// for back to the pending set, so a later turn delivers them.
//
// This is the end-of-run settle, and it keys off the conversation rather than
// the run's error on purpose: seed can exit without draining its injection
// buffer on paths that report no error (a force-finalize after the last allowed
// continuation returns a clean result), so a nil error is not evidence anything
// was consumed.
func (a *Agent) releaseUnlandedSteer(counted func(stamped string) int) int {
	return a.settleAcceptedSteer(counted, true)
}

// hasAcceptedSteer reports whether any entry awaits landing proof.
func hasAcceptedSteer(entries []pendingSteer) bool {
	for i := range entries {
		if entries[i].accepted {
			return true
		}
	}
	return false
}

// releaseStagedSteer un-marks an in-flight entry after seed REJECTED it,
// keeping the message staged and retractable for the next boundary.
func (a *Agent) releaseStagedSteer(id uint64) {
	if a == nil {
		return
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := range ss.entries {
		if ss.entries[i].id == id && ss.entries[i].inFlight {
			ss.entries[i].inFlight = false
			return
		}
	}
}

// RetractLatestSteer removes the newest staged (not yet delivered to seed)
// entry and returns its content. This is the "pull the steer message back
// into editing" primitive: once seed has accepted a message it is in the
// conversation pipeline and cannot be revised. Entries currently in-flight
// (being handed to seed at this instant) are skipped — retraction there is
// deterministically too late.
func (a *Agent) RetractLatestSteer() (string, bool) {
	if a == nil {
		return "", false
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := len(ss.entries) - 1; i >= 0; i-- {
		// inFlight: mid-hand-off this instant. accepted: already in seed's
		// buffer, so it is either about to enter the conversation or lost with
		// the run — retraction can't be honoured either way.
		if ss.entries[i].inFlight || ss.entries[i].accepted {
			continue
		}
		content := ss.entries[i].content
		ss.entries = append(ss.entries[:i], ss.entries[i+1:]...)
		return content, true
	}
	return "", false
}

// PendingSteerCount returns the number of staged entries still awaiting
// hand-off to seed (the retractable set, plus any rejected-then-released
// ones). Accepted entries are excluded: from the user's side the message has
// left the queue, which is what the queue badge shows.
func (a *Agent) PendingSteerCount() int {
	if a == nil {
		return 0
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	n := 0
	for _, e := range ss.entries {
		if !e.inFlight && !e.accepted {
			n++
		}
	}
	return n
}

// clearSteerStaging wipes the staging queue (ClearInputInjectionContext).
func (a *Agent) clearSteerStaging() {
	if a == nil {
		return
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.entries = nil
}
