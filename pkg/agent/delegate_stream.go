package agent

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// DelegateStreamBridge bridges events from a delegate agent to the parent's event bus.
// It subscribes to the delegate's events and republishes them as delegate_activity events.
type DelegateStreamBridge struct {
	parentAgent  *Agent
	delegateID   string
	result       *DelegateResult
	mu           sync.Mutex
	started      atomic.Bool
	tokenUsage   int
	cost         float64
	toolCalls    []ToolCallRecord
	filesChanged []string
	startTime    time.Time
	iterations   int

}

// NewDelegateStreamBridge creates a new stream bridge for a delegate agent
func NewDelegateStreamBridge(parentAgent *Agent, delegateID string) *DelegateStreamBridge {
	return &DelegateStreamBridge{
		parentAgent: parentAgent,
		delegateID:  delegateID,
		startTime:   time.Now(),
	}
}

// Start begins bridging events from the delegate to the parent
func (b *DelegateStreamBridge) Start() {
	if !b.started.CompareAndSwap(false, true) {
		return // already started
	}
	b.startTime = time.Now()
	// Event subscription would happen here when event bus integration is complete
	// For now, the bridge tracks state for result computation
}

// Stop stops the bridge and finalizes the result
func (b *DelegateStreamBridge) Stop() {
	if !b.started.CompareAndSwap(true, false) {
		return // not started
	}
}

// RecordToolCall records a tool call made by the delegate
func (b *DelegateStreamBridge) RecordToolCall(tool, input, output string, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolCalls = append(b.toolCalls, ToolCallRecord{
		ToolName:  tool,
		Input:     input,
		Output:    output,
		Timestamp: time.Now(),
		Success:   success,
	})
}

// RecordFileChange records a file that was modified by the delegate
func (b *DelegateStreamBridge) RecordFileChange(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.filesChanged {
		if f == path {
			return
		}
	}
	b.filesChanged = append(b.filesChanged, path)
}

// RecordTokenUsage accumulates token usage from the delegate
func (b *DelegateStreamBridge) RecordTokenUsage(tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokenUsage += tokens
}

// RecordCost accumulates cost from the delegate
func (b *DelegateStreamBridge) RecordCost(cost float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cost += cost
}

// RecordIteration increments the iteration counter
func (b *DelegateStreamBridge) RecordIteration() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.iterations++
}

// GetResult returns the final DelegateResult
func (b *DelegateStreamBridge) GetResult(summary string, exitStatus string, errorMessage string) *DelegateResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	return &DelegateResult{
		Summary:      summary,
		FilesChanged: append([]string{}, b.filesChanged...),
		ToolsCalled:  append([]ToolCallRecord{}, b.toolCalls...),
		TokensUsed:   b.tokenUsage,
		Cost:         b.cost,
		Iterations:   b.iterations,
		ExitStatus:   exitStatus,
		ErrorMessage: errorMessage,
	}
}

// PublishActivity publishes a delegate_activity event to the parent's event bus
func (b *DelegateStreamBridge) PublishActivity(action, summary string, depth int) {
	if b.parentAgent == nil || b.parentAgent.eventBus == nil {
		return
	}

	event := events.DelegateActivityEvent(b.delegateID, action, summary, depth)
	b.parentAgent.eventBus.Publish(events.EventTypeDelegateActivity, event)
}
