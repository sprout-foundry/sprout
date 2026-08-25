package agent

import (
	"context"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSteeringChannelAccessor verifies that SteeringChannel() returns the
// inputInjectionChan and nil for a nil agent.
func TestSteeringChannelAccessor(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	ch := parent.SteeringChannel()
	if ch == nil {
		t.Fatal("SteeringChannel() should not return nil for a valid agent")
	}
	// It should be the same channel as GetInputInjectionContext
	if ch != parent.GetInputInjectionContext() {
		t.Fatal("SteeringChannel() should return the same channel as GetInputInjectionContext()")
	}

	// Nil agent should return nil channel
	var nilAgent *Agent
	if nilAgent.SteeringChannel() != nil {
		t.Fatal("SteeringChannel() should return nil for a nil agent")
	}
}

// TestInjectInputIntoActivePrefersPrimary verifies that steering messages
// go to the primary agent FIRST, even when subagents are running.
// This is the core regression test for SP-094-8.
func TestInjectInputIntoActivePrefersPrimary(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// Register a fake running subagent directly (no need to start an actual subagent).
	fakeSubAgent := newIsolatedTestAgent(t)
	defer fakeSubAgent.Shutdown()

	runner.active.Store("fake-sub-1", &runningSubagent{
		ID:        "fake-sub-1",
		Persona:   "coder",
		Prompt:    "do something",
		StartedAt: time.Now(),
		Agent:     fakeSubAgent,
		Ctx:       context.Background(),
		Cancel:    func() {},
	})

	// Inject a steering message — should go to PRIMARY, not the subagent.
	targetID, ok := runner.InjectInputIntoActive("commit and push")
	if !ok {
		t.Fatal("InjectInputIntoActive should have delivered the message")
	}
	if targetID != "primary" {
		t.Fatalf("expected target 'primary', got %q", targetID)
	}

	// Verify the message landed on the PRIMARY's channel, not the subagent's.
	select {
	case msg := <-parent.SteeringChannel():
		if msg != "commit and push" {
			t.Fatalf("expected 'commit and push', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("steer message did not arrive on primary's channel within 100ms")
	}

	// Verify the subagent did NOT receive the message.
	select {
	case <-fakeSubAgent.SteeringChannel():
		t.Fatal("subagent should NOT have received the steer message")
	default:
		// Good — subagent channel is empty.
	}
}

// TestInjectInputIntoActiveFallbackToSubagent verifies that when the
// primary's channel is full, the message falls back to the deepest
// running subagent.
func TestInjectInputIntoActiveFallbackToSubagent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// Fill the primary's input channel to force fallback.
	for i := 0; i < inputInjectionBufferSize; i++ {
		parent.InjectInputContext("filler")
	}

	// Register a fake running subagent.
	fakeSubAgent := newIsolatedTestAgent(t)
	defer fakeSubAgent.Shutdown()

	runner.active.Store("fake-sub-2", &runningSubagent{
		ID:        "fake-sub-2",
		Persona:   "tester",
		Prompt:    "run tests",
		StartedAt: time.Now(),
		Agent:     fakeSubAgent,
		Ctx:       context.Background(),
		Cancel:    func() {},
	})

	// Inject a steering message — primary is full, so should go to subagent.
	targetID, ok := runner.InjectInputIntoActive("fix this bug")
	if !ok {
		t.Fatal("InjectInputIntoActive should have delivered to subagent as fallback")
	}
	if targetID != "fake-sub-2" {
		t.Fatalf("expected target 'fake-sub-2', got %q", targetID)
	}

	// Verify the subagent received the message.
	select {
	case msg := <-fakeSubAgent.SteeringChannel():
		if msg != "fix this bug" {
			t.Fatalf("expected 'fix this bug', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("steer message did not arrive on subagent's channel within 100ms")
	}
}

// TestInjectInputIntoActiveNoTargets verifies that when neither primary
// nor subagent can accept input, the message is dropped gracefully.
func TestInjectInputIntoActiveNoTargets(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// Fill primary's channel.
	for i := 0; i < inputInjectionBufferSize; i++ {
		parent.InjectInputContext("filler")
	}

	// No active subagents registered.
	targetID, ok := runner.InjectInputIntoActive("orphan message")
	if ok {
		t.Fatalf("expected delivery to fail, got target %q", targetID)
	}
	if targetID != "" {
		t.Fatalf("expected empty target ID, got %q", targetID)
	}
}

// TestInjectInputIntoActiveNilRunner verifies nil-safety.
func TestInjectInputIntoActiveNilRunner(t *testing.T) {
	var runner *SubagentRunner
	targetID, ok := runner.InjectInputIntoActive("test")
	if ok {
		t.Fatalf("nil runner should return false, got target %q", targetID)
	}
}

// TestInjectInputIntoActiveEmptyInput verifies empty input is rejected.
func TestInjectInputIntoActiveEmptyInput(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	targetID, ok := runner.InjectInputIntoActive("")
	if ok {
		t.Fatalf("empty input should return false, got target %q", targetID)
	}
}

// TestInjectInputIntoActiveRoutesToDeepestSubagent verifies that when
// falling back to subagents (primary full), the deepest (most recently
// started) subagent receives the message.
func TestInjectInputIntoActiveRoutesToDeepestSubagent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// Fill primary's channel.
	for i := 0; i < inputInjectionBufferSize; i++ {
		parent.InjectInputContext("filler")
	}

	// Register two subagents: older and newer.
	olderSub := newIsolatedTestAgent(t)
	defer olderSub.Shutdown()
	newerSub := newIsolatedTestAgent(t)
	defer newerSub.Shutdown()

	runner.active.Store("older-sub", &runningSubagent{
		ID:        "older-sub",
		Persona:   "coder",
		Prompt:    "older task",
		StartedAt: time.Now().Add(-1 * time.Second),
		Agent:     olderSub,
		Ctx:       context.Background(),
		Cancel:    func() {},
	})
	runner.active.Store("newer-sub", &runningSubagent{
		ID:        "newer-sub",
		Persona:   "tester",
		Prompt:    "newer task",
		StartedAt: time.Now(),
		Agent:     newerSub,
		Ctx:       context.Background(),
		Cancel:    func() {},
	})

	targetID, ok := runner.InjectInputIntoActive("steer deepest")
	if !ok {
		t.Fatal("should have delivered to deepest subagent")
	}
	if targetID != "newer-sub" {
		t.Fatalf("expected 'newer-sub' (deepest), got %q", targetID)
	}

	// Verify the newer subagent received the message.
	select {
	case msg := <-newerSub.SteeringChannel():
		if msg != "steer deepest" {
			t.Fatalf("expected 'steer deepest', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("message did not arrive on newer subagent's channel")
	}

	// Verify the older subagent did NOT receive it.
	select {
	case <-olderSub.SteeringChannel():
		t.Fatal("older subagent should NOT have received the message")
	default:
		// Good.
	}
}

// TestInjectInputIntoActiveSkipsCompletedSubagent verifies that completed
// subagents are skipped when looking for fallback targets.
func TestInjectInputIntoActiveSkipsCompletedSubagent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// Fill primary's channel.
	for i := 0; i < inputInjectionBufferSize; i++ {
		parent.InjectInputContext("filler")
	}

	// Register a completed subagent and an active one.
	completedSub := newIsolatedTestAgent(t)
	defer completedSub.Shutdown()
	activeSub := newIsolatedTestAgent(t)
	defer activeSub.Shutdown()

	completed := &runningSubagent{
		ID:        "completed-sub",
		Persona:   "coder",
		Prompt:    "done task",
		StartedAt: time.Now(),
		Agent:     completedSub,
		Ctx:       context.Background(),
		Cancel:    func() {},
	}
	completed.Completed.Store(true)
	runner.active.Store("completed-sub", completed)

	runner.active.Store("active-sub", &runningSubagent{
		ID:        "active-sub",
		Persona:   "tester",
		Prompt:    "active task",
		StartedAt: time.Now().Add(-1 * time.Second),
		Agent:     activeSub,
		Ctx:       context.Background(),
		Cancel:    func() {},
	})

	targetID, ok := runner.InjectInputIntoActive("steer active")
	if !ok {
		t.Fatal("should have delivered to active subagent")
	}
	if targetID != "active-sub" {
		t.Fatalf("expected 'active-sub', got %q", targetID)
	}
}
