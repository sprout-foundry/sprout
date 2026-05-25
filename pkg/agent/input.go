package agent

import (
	"sync"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// deferredQueue holds steer messages that the user typed while a turn
// was running but chose to defer until the NEXT user-prompted turn
// rather than inject mid-flight (SP-055 Phase 3b). The CLI's REPL
// loop drains this queue and joins the entries with the user's next
// typed prompt before calling ProcessQuery.
//
// Distinct from inputInjectionChan (which seed consumes mid-turn) so
// the two delivery semantics never collide: a message goes to one
// channel or the other based on the user's submit mode.
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

// EnqueueDeferredMessage appends a steer message to be consumed at the
// start of the next user-prompted turn. Order is FIFO. No upper bound
// is enforced — practical sessions accumulate at most a handful before
// the user submits, but we cap defensively at 32 to avoid runaway
// growth from a stuck loop.
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

// DrainDeferredMessages atomically removes and returns all queued
// messages. The CLI's REPL loop calls this after ReadLine() returns
// the user's next prompt and prepends them to the typed text.
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

// DeferredMessageCount returns how many messages are currently queued.
// Used by the UI to show "N queued" hints. Reads are racy with
// enqueues but counts are advisory anyway.
func (a *Agent) DeferredMessageCount() int {
	if a == nil {
		return 0
	}
	q := a.deferredQueue()
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// InjectInputContext injects a new user input using context-based interrupt system
func (a *Agent) InjectInputContext(input string) error {
	a.inputInjectionMutex.Lock()
	defer a.inputInjectionMutex.Unlock()

	// Send the new input to the injection channel
	select {
	case a.inputInjectionChan <- input:
		return nil
	default:
		return agenterrors.NewTransientError("failed to inject input: input injection channel is full", nil)
	}
}

// GetInputInjectionContext returns the input injection channel for the new system
func (a *Agent) GetInputInjectionContext() <-chan string {
	return a.inputInjectionChan
}

// ClearInputInjectionContext clears any pending input injections
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
			return
		}
	}
}

// IsInterrupted returns true if an interrupt has been requested
func (a *Agent) IsInterrupted() bool {
	return a.CheckForInterrupt()
}
