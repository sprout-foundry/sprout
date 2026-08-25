package agent

import (
	"sync"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// steerStageCap bounds the pending-steer queue. Matches inputInjectionBufferSize
// so staging rejects overflow at the same point the legacy channel did.
const steerStageCap = 10

// pendingSteer is one user-steer message waiting to be delivered into seed.
type pendingSteer struct {
	id       uint64 // monotonic per-agent; commit-by-id avoids retract races
	content  string
	inFlight bool // being handed to seed this instant; not retractable
}

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

// peekStagedSteer marks the oldest staged entry in-flight and returns it.
// The entry stays in the list (for FIFO visibility) but becomes invisible
// to RetractLatestSteer until commitStagedSteer removes it — once seed's
// boundary is handing the message over, retraction is deterministically
// "too late" rather than a race outcome. If seed rejects the message,
// releaseStagedSteer clears the flag so it stays retractable.
func (a *Agent) peekStagedSteer() (uint64, string, bool) {
	if a == nil {
		return 0, "", false
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := range ss.entries {
		if ss.entries[i].inFlight {
			return 0, "", false
		}
		ss.entries[i].inFlight = true
		return ss.entries[i].id, ss.entries[i].content, true
	}
	return 0, "", false
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

// commitStagedSteer removes the entry with the given id. It is called only
// after seed accepted the message (InjectInput returned true), which is the
// point of no retraction. Unknown ids (already retracted) are a no-op.
func (a *Agent) commitStagedSteer(id uint64) {
	if a == nil {
		return
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for i := range ss.entries {
		if ss.entries[i].id == id {
			ss.entries = append(ss.entries[:i], ss.entries[i+1:]...)
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
		if ss.entries[i].inFlight {
			continue
		}
		content := ss.entries[i].content
		ss.entries = append(ss.entries[:i], ss.entries[i+1:]...)
		return content, true
	}
	return "", false
}

// PendingSteerCount returns the number of staged entries not currently
// in flight (the retractable set, plus any rejected-then-released ones).
func (a *Agent) PendingSteerCount() int {
	if a == nil {
		return 0
	}
	ss := a.getSteerStage()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	n := 0
	for _, e := range ss.entries {
		if !e.inFlight {
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
