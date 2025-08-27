package ui

import (
	"fmt"
	"sync"
	"time"
)

// Event is a marker for all UI events.
type Event interface{}

// LogEvent represents a log line to display in the UI.
type LogEvent struct {
	Level string
	Text  string
	Time  time.Time
}

// ProgressRow represents a single agent row in the progress table
type ProgressRow struct {
	Name   string
	Status string
	Step   string
	Tokens int
	Cost   float64
}

// ProgressSnapshotEvent represents a full table snapshot
type ProgressSnapshotEvent struct {
	Completed   int
	Total       int
	Rows        []ProgressRow
	Time        time.Time
	TotalTokens int
	TotalCost   float64
	BaseModel   string
}

// StatusEvent carries a concise current activity/status message for the TUI header/body
type StatusEvent struct{ Text string }

// StreamStartedEvent indicates a streaming operation has begun
type StreamStartedEvent struct{}

// StreamEndedEvent indicates a streaming operation has ended
type StreamEndedEvent struct{}

// PromptRequestEvent asks the UI to collect user input
type PromptRequestEvent struct {
	ID     string
	Prompt string
	// Optional long-form context (e.g., code diff) to display in a scrollable viewport
	Context      string
	RequireYesNo bool
	DefaultYes   bool
}

// PromptResponseEvent carries the user's response
type PromptResponseEvent struct {
	ID        string
	Text      string
	Confirmed bool
}

var eventChan = make(chan Event, 2048)

// waiter registry for prompt responses
var (
	waitersMu       sync.Mutex
	responseWaiters = map[string]chan PromptResponseEvent{}
)

// Events exposes a receive-only channel of events.
func Events() <-chan Event { return eventChan }

// Publish sends an event to the UI if possible (drops on full buffer).
func Publish(event Event) {
	select {
	case eventChan <- event:
	default:
		// drop if buffer is full to avoid blocking
	}
}

// Log publishes a plain info log line.
func Log(text string) { Publish(LogEvent{Level: "info", Text: text, Time: time.Now()}) }

// Logf publishes a formatted log line.
func Logf(format string, args ...any) { Log(fmt.Sprintf(format, args...)) }

// PublishStatus sends a concise status message for display in the UI
func PublishStatus(text string) { Publish(StatusEvent{Text: text}) }

// PublishProgress broadcasts a progress snapshot to the UI
func PublishProgress(completed, total int, rows []ProgressRow) {
	Publish(ProgressSnapshotEvent{
		Completed: completed,
		Total:     total,
		Rows:      rows,
		Time:      time.Now(),
	})
}

// PublishProgressWithTokens broadcasts a progress snapshot with token/cost info to the UI
func PublishProgressWithTokens(completed, total, totalTokens int, totalCost float64, baseModel string, rows []ProgressRow) {
	Publish(ProgressSnapshotEvent{
		Completed:   completed,
		Total:       total,
		Rows:        rows,
		Time:        time.Now(),
		TotalTokens: totalTokens,
		TotalCost:   totalCost,
		BaseModel:   baseModel,
	})
}

// ModelInfoEvent announces the active or base model name
type ModelInfoEvent struct{ Name string }

// PublishModel informs the UI about the current model
func PublishModel(name string) { Publish(ModelInfoEvent{Name: name}) }

// PublishStreamStarted signals that a stream started
func PublishStreamStarted() { Publish(StreamStartedEvent{}) }

// PublishStreamEnded signals that a stream ended
func PublishStreamEnded() { Publish(StreamEndedEvent{}) }

// RegisterPromptWaiter creates a channel to wait for a prompt response
func RegisterPromptWaiter(id string) chan PromptResponseEvent {
	waitersMu.Lock()
	defer waitersMu.Unlock()
	ch := make(chan PromptResponseEvent, 1)
	responseWaiters[id] = ch
	return ch
}

// SubmitPromptResponse delivers a response to any waiter and publishes the event
func SubmitPromptResponse(id string, text string, confirmed bool) {
	resp := PromptResponseEvent{ID: id, Text: text, Confirmed: confirmed}
	waitersMu.Lock()
	if ch, ok := responseWaiters[id]; ok {
		ch <- resp
		close(ch)
		delete(responseWaiters, id)
	}
	waitersMu.Unlock()
	Publish(resp)
}

// PromptYesNo requests a yes/no answer from the user via UI when enabled
func PromptYesNo(prompt string, defaultYes bool) (bool, error) {
	id := fmt.Sprintf("p-%d", time.Now().UnixNano())
	ch := RegisterPromptWaiter(id)
	Publish(PromptRequestEvent{ID: id, Prompt: prompt, Context: "", RequireYesNo: true, DefaultYes: defaultYes})
	select {
	case resp := <-ch:
		return resp.Confirmed, nil
	case <-time.After(5 * time.Minute):
		return defaultYes, fmt.Errorf("prompt timed out")
	}
}

// PromptText requests a line of input from the user via UI
func PromptText(prompt string) (string, error) {
	id := fmt.Sprintf("p-%d", time.Now().UnixNano())
	ch := RegisterPromptWaiter(id)
	Publish(PromptRequestEvent{ID: id, Prompt: prompt, Context: "", RequireYesNo: false})
	select {
	case resp := <-ch:
		return resp.Text, nil
	case <-time.After(30 * time.Minute):
		return "", fmt.Errorf("prompt timed out")
	}
}

// PromptYesNoWithContext displays a yes/no modal with a scrollable context area
func PromptYesNoWithContext(prompt string, context string, defaultYes bool) (bool, error) {
	id := fmt.Sprintf("p-%d", time.Now().UnixNano())
	ch := RegisterPromptWaiter(id)
	Publish(PromptRequestEvent{ID: id, Prompt: prompt, Context: context, RequireYesNo: true, DefaultYes: defaultYes})
	select {
	case resp := <-ch:
		return resp.Confirmed, nil
	case <-time.After(5 * time.Minute):
		return defaultYes, fmt.Errorf("prompt timed out")
	}
}

// PromptTextWithContext displays a free-text modal with a scrollable context area
func PromptTextWithContext(prompt string, context string) (string, error) {
	id := fmt.Sprintf("p-%d", time.Now().UnixNano())
	ch := RegisterPromptWaiter(id)
	Publish(PromptRequestEvent{ID: id, Prompt: prompt, Context: context, RequireYesNo: false})
	select {
	case resp := <-ch:
		return resp.Text, nil
	case <-time.After(30 * time.Minute):
		return "", fmt.Errorf("prompt timed out")
	}
}
