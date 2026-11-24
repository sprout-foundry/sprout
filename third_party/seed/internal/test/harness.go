package test

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/events"
)

// Harness is an end-to-end test harness for the seed agent.
// It wires up mock Provider, ToolExecutor, UI, and EventBus so you can
// script full conversation flows and assert on every interface interaction.
//
// Usage:
//
//	h := test.NewHarness()
//	h.Provider().AddTextResponse("Hello!")
//	agent := h.NewAgent()
//	result, err := agent.Run(ctx, "Hi")
//	h.AssertNoError(err)
//	h.AssertEquals(result, "Hello!")
type Harness struct {
	provider *MockProvider
	executor *MockExecutor
	ui       *MockUI
	bus      *events.EventBus

	// Collected events for assertion
	eventChan <-chan events.UIEvent
	events    []events.UIEvent

	// Test helper
	t interface {
		Errorf(format string, args ...interface{})
		Fatal(args ...interface{})
		Fatalf(format string, args ...interface{})
	}
	hasT bool
}

// NewHarness creates a new test harness.
func NewHarness() *Harness {
	h := &Harness{
		provider: NewMockProvider(),
		executor: NewMockExecutor(),
		ui:       NewMockUI(),
		bus:      events.NewEventBus(),
	}

	// Subscribe once — the event bus broadcasts to ALL subscribers regardless of name,
	// so one subscription catches everything.
	h.eventChan = h.bus.Subscribe("__test_catchall__")

	return h
}

// NewHarnessWithT creates a harness that can call t.Fatalf directly.
func NewHarnessWithT(t interface {
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
}) *Harness {
	h := NewHarness()
	h.t = t
	h.hasT = true
	return h
}

// Provider returns the mock provider for configuration.
func (h *Harness) Provider() *MockProvider {
	return h.provider
}

// Executor returns the mock executor for configuration.
func (h *Harness) Executor() *MockExecutor {
	return h.executor
}

// UI returns the mock UI for configuration.
func (h *Harness) UI() *MockUI {
	return h.ui
}

// EventBus returns the event bus.
func (h *Harness) EventBus() *events.EventBus {
	return h.bus
}

// NewAgent creates a new agent wired to the harness mocks.
func (h *Harness) NewAgent() *core.Agent {
	agent, err := core.NewAgent(core.Options{
		Provider:       h.provider,
		Executor:       h.executor,
		UI:             h.ui,
		EventPublisher: h.bus,
	})
	if err != nil {
		panic(fmt.Sprintf("NewAgent failed: %v", err))
	}
	return agent
}

// NewAgentWithOptions creates an agent with custom options.
// Provider and Executor are set from the harness; other options are from opts.
func (h *Harness) NewAgentWithOptions(opts core.Options) *core.Agent {
	opts.Provider = h.provider
	opts.Executor = h.executor
	if opts.UI == nil {
		opts.UI = h.ui
	}
	if opts.EventPublisher == nil {
		opts.EventPublisher = h.bus
	}
	agent, err := core.NewAgent(opts)
	if err != nil {
		panic(fmt.Sprintf("NewAgentWithOptions failed: %v", err))
	}
	return agent
}

// Run is a convenience method: creates an agent, runs a query, returns result.
func (h *Harness) Run(query string) (string, error) {
	return h.NewAgent().Run(context.Background(), query)
}

// RunWithAgent runs a query on the given agent.
func (h *Harness) RunWithAgent(agent *core.Agent, query string) (string, error) {
	return agent.Run(context.Background(), query)
}

// --- Assertions ---

// AssertNoError fails the test if err is not nil.
func (h *Harness) AssertNoError(err error) {
	if err != nil {
		h.fail("expected no error, got: %v", err)
	}
}

// AssertError fails the test if err is nil.
func (h *Harness) AssertError(err error) {
	if err == nil {
		h.fail("expected error, got nil")
	}
}

// AssertErrorContains fails if err doesn't contain the substring.
func (h *Harness) AssertErrorContains(err error, substr string) {
	if err == nil {
		h.fail("expected error containing %q, got nil", substr)
		return
	}
	if !strings.Contains(err.Error(), substr) {
		h.fail("expected error containing %q, got: %v", substr, err)
	}
}

// AssertEquals fails if got != want.
func (h *Harness) AssertEquals(got, want string) {
	if got != want {
		h.fail("expected %q, got %q", want, got)
	}
}

// AssertContains fails if s doesn't contain substr.
func (h *Harness) AssertContains(s, substr string) {
	if !strings.Contains(s, substr) {
		h.fail("expected %q to contain %q", s, substr)
	}
}

// AssertProviderCalledN times.
func (h *Harness) AssertProviderCalledN(n int) {
	got := h.provider.CallCount()
	if got != n {
		h.fail("expected provider called %d times, got %d", n, got)
	}
}

// AssertExecutorCalledN times.
func (h *Harness) AssertExecutorCalledN(n int) {
	got := h.executor.CallCount()
	if got != n {
		h.fail("expected executor called %d times, got %d", n, got)
	}
}

// AssertLastRequestHasNMessages checks the message count in the last provider request.
func (h *Harness) AssertLastRequestHasNMessages(n int) {
	req := h.provider.LastRequest()
	if req == nil {
		h.fail("expected provider to have been called")
		return
	}
	if len(req.Messages) != n {
		h.fail("expected %d messages in last request, got %d", n, len(req.Messages))
	}
}

// AssertFirstMessageIsSystem checks that the first message in the last request is a system message.
func (h *Harness) AssertFirstMessageIsSystem() {
	req := h.provider.LastRequest()
	if req == nil {
		h.fail("expected provider to have been called")
		return
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		h.fail("expected first message to be system, got %v", req.Messages[0].Role)
	}
}

// AssertSystemPromptEquals checks the system prompt content.
func (h *Harness) AssertSystemPromptEquals(want string) {
	req := h.provider.LastRequest()
	if req == nil || len(req.Messages) == 0 {
		h.fail("expected provider to have been called with messages")
		return
	}
	got := req.Messages[0].Content
	if got != want {
		h.fail("expected system prompt %q, got %q", want, got)
	}
}

// AssertStateHasNMessages checks the agent state message count.
func (h *Harness) AssertStateHasNMessages(agent *core.Agent, n int) {
	got := agent.State().Len()
	if got != n {
		h.fail("expected state to have %d messages, got %d", n, got)
	}
}

// AssertStateHasTokens checks the total token count in state.
func (h *Harness) AssertStateHasTokens(agent *core.Agent, want int) {
	got := agent.State().TotalTokens()
	if got != want {
		h.fail("expected %d total tokens, got %d", want, got)
	}
}

// AssertSessionID checks the session ID.
func (h *Harness) AssertSessionID(agent *core.Agent, want string) {
	got := agent.State().SessionID()
	if got != want {
		h.fail("expected session ID %q, got %q", want, got)
	}
}

// AssertSessionIDNotEmpty checks that the session ID is non-empty.
func (h *Harness) AssertSessionIDNotEmpty(agent *core.Agent) {
	got := agent.State().SessionID()
	if got == "" {
		h.fail("expected non-empty session ID")
	}
}

// AssertEventPublished checks that an event of the given type was published.
func (h *Harness) AssertEventPublished(eventType string) {
	// Drain all buffered events from the channel into our collection
	done := false
	for !done {
		select {
		case ev := <-h.eventChan:
			h.events = append(h.events, ev)
		default:
			done = true
		}
	}
	found := false
	for _, ev := range h.events {
		if ev.Type == eventType {
			found = true
			break
		}
	}
	if !found {
		h.fail("expected event type %q to be published; got: %v", eventType, h.eventTypes())
	}
}

// FindEvents returns all collected events of the given type.
func (h *Harness) FindEvents(eventType string) []events.UIEvent {
	// Drain all buffered events from the channel into our collection
	done := false
	for !done {
		select {
		case ev := <-h.eventChan:
			h.events = append(h.events, ev)
		default:
			done = true
		}
	}
	var matched []events.UIEvent
	for _, ev := range h.events {
		if ev.Type == eventType {
			matched = append(matched, ev)
		}
	}
	return matched
}

// AssertNoEventsPublished checks that no events were published.
func (h *Harness) AssertNoEventsPublished() {
	// Drain channel first
	done := false
	for !done {
		select {
		case ev := <-h.eventChan:
			h.events = append(h.events, ev)
		default:
			done = true
		}
	}
	if len(h.events) > 0 {
		h.fail("expected no events, got %d", len(h.events))
	}
}

// AssertLastRequestHasTool checks that the last request included a tool with the given name.
func (h *Harness) AssertLastRequestHasTool(name string) {
	req := h.provider.LastRequest()
	if req == nil {
		h.fail("expected provider to have been called")
		return
	}
	found := false
	for _, tool := range req.Tools {
		if tool.Function.Name == name {
			found = true
			break
		}
	}
	if !found {
		h.fail("expected tool %q in last request, got: %v", name, h.toolNames(req.Tools))
	}
}

// Reset clears all mock state for reuse.
func (h *Harness) Reset() {
	h.provider.Reset()
	h.executor.Reset()
	h.ui.Reset()
	h.events = nil
	// Drain any buffered events from the channel
	for {
		select {
		case <-h.eventChan:
		default:
			return
		}
	}
}

// fail reports a test failure.
func (h *Harness) fail(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if h.hasT {
		h.t.Fatalf(msg)
	} else {
		panic(msg)
	}
}

func (h *Harness) eventTypes() []string {
	types := make([]string, len(h.events))
	for i, ev := range h.events {
		types[i] = ev.Type
	}
	return types
}

func (h *Harness) toolNames(tools []core.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Function.Name
	}
	return names
}
