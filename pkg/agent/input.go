package agent

import (
	"sync"
) // deferredQueue holds steer messages deferred until the NEXT user-prompted turn.
// Distinct from inputInjectionChan so the two delivery semantics never collide.
type deferredQueue struct {
	mu    sync.Mutex
	items []string
}

var agentDeferredQueues sync.Map // *Agent → *deferredQueue

func (a *Agent) deferredQueue() *deferredQueue {
	if v, ok := agentDeferredQueues.Load(a); ok {
		return v.(*deferredQueue)
	}
	q := &deferredQueue{}
	actual, _ := agentDeferredQueues.LoadOrStore(a, q)
	return actual.(*deferredQueue)
}

// EnqueueDeferredMessage appends a steer message that auto-submits as its
// own turn when the current turn ends. FIFO, capped at 32.
const deferredQueueCap = 32

func (a *Agent) EnqueueDeferredMessage(text string) {
	if a == nil || text == "" {
		return
	}
	q := a.deferredQueue()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, text)
	if over := len(q.items) - deferredQueueCap; over > 0 {
		q.items = q.items[over:]
	}
}

// DrainDeferredMessages atomically removes and returns all queued messages. Used by the CLI REPL loop.
func (a *Agent) DrainDeferredMessages() []string {
	if a == nil {
		return nil
	}
	q := a.deferredQueue()
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	out := q.items
	q.items = nil
	return out
}

// DeferredMessageCount returns the number of queued messages (advisory only).
func (a *Agent) DeferredMessageCount() int {
	if a == nil {
		return 0
	}
	q := a.deferredQueue()
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// RetractLatestDeferredMessage removes and returns the newest queued message.
// Queue messages sit in the queue until the current turn ends (they then
// auto-run), so any of them is retractable mid-turn. This powers steer-panel
// recall.
func (a *Agent) RetractLatestDeferredMessage() (string, bool) {
	if a == nil {
		return "", false
	}
	q := a.deferredQueue()
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return "", false
	}
	last := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return last, true
}

// InjectInputContext injects a new user input using the context-based interrupt system.
// Delivery goes through the retractable staging queue (steer_staging.go):
// the message sits staged until a conversation-loop boundary hands it to
// seed. Until that moment it can be pulled back with RetractLatestSteer.
func (a *Agent) InjectInputContext(input string) error {
	return a.StageSteerInput(input)
}

// GetInputInjectionContext returns the input injection channel for the new system
func (a *Agent) GetInputInjectionContext() <-chan string {
	return a.inputInjectionChan
}

// SteeringChannel returns the receive-only input channel for steer/queue messages.
// Subagent plumbing consults this channel FIRST before falling back to its own input channel.
func (a *Agent) SteeringChannel() <-chan string {
	if a == nil {
		return nil
	}
	return a.inputInjectionChan
}

// ClearInputInjectionContext clears any pending input injections.
//
// Lock ordering invariant: steerStage.mu is never held while
// inputInjectionMutex is acquired. StageSteerInput releases steerStage.mu
// before its non-blocking channel mirror; here inputInjectionMutex is held
// only during the channel drain and steerStage.mu is acquired afterwards.
func (a *Agent) ClearInputInjectionContext() {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Drain the channel
	for {
		select {
		case <-a.inputInjectionChan:
			// Remove item
		default:
			// Channel empty
			a.clearSteerStaging()
			return
		}
	}
}

// IsInterrupted returns true if an interrupt has been requested
func (a *Agent) IsInterrupted() bool {
	return a.CheckForInterrupt()
}
