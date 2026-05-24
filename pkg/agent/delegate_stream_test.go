package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestAgentForStreamBridge creates a minimal Agent with an event bus for testing
// PublishActivity behavior in the delegate stream bridge.
func newTestAgentForStreamBridge(t *testing.T) (*Agent, *events.EventBus) {
	t.Helper()

	bus := events.NewEventBus()

	agent := &Agent{
		output:       NewAgentOutputManager(),
		state:        NewAgentStateManager(false),
		security:     NewAgentSecurityManager(),
		interruptCtx: nil,
		eventBus:     bus,
	}

	return agent, bus
}

func TestNewDelegateStreamBridge(t *testing.T) {
	parent := &Agent{}
	bridge := NewDelegateStreamBridge(parent, "test-delegate-1")

	require.NotNil(t, bridge)
	assert.Equal(t, "test-delegate-1", bridge.delegateID)
	assert.Equal(t, parent, bridge.parentAgent)
	assert.False(t, bridge.started.Load())
	assert.Empty(t, bridge.filesChanged)
	assert.Empty(t, bridge.toolCalls)
	assert.Equal(t, 0, bridge.tokenUsage)
	assert.Equal(t, 0.0, bridge.cost)
	assert.Equal(t, 0, bridge.iterations)
	assert.True(t, bridge.startTime.After(time.Now().Add(-time.Hour)))
}

func TestDelegateStreamBridge_Start(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
	assert.False(t, bridge.started.Load())

	bridge.Start()
	assert.True(t, bridge.started.Load())
}

func TestDelegateStreamBridge_Start_Idempotent(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.Start()
	bridge.Start()
	bridge.Start()

	assert.True(t, bridge.started.Load())
}

func TestDelegateStreamBridge_Stop(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
	bridge.Start()
	assert.True(t, bridge.started.Load())

	bridge.Stop()
	assert.False(t, bridge.started.Load())
}

func TestDelegateStreamBridge_Stop_Idempotent(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
	bridge.Start()

	bridge.Stop()
	bridge.Stop()
	bridge.Stop()

	assert.False(t, bridge.started.Load())
}

func TestDelegateStreamBridge_StopWithoutStart(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
	assert.False(t, bridge.started.Load())

	bridge.Stop() // CAS will fail since started is false
	assert.False(t, bridge.started.Load())
}

func TestDelegateStreamBridge_RecordToolCall(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordToolCall("read_file", "{\"path\":\"foo.go\"}", "content", true)

	require.Len(t, bridge.toolCalls, 1)
	assert.Equal(t, "read_file", bridge.toolCalls[0].ToolName)
	assert.Equal(t, `{"path":"foo.go"}`, bridge.toolCalls[0].Input)
	assert.Equal(t, "content", bridge.toolCalls[0].Output)
	assert.True(t, bridge.toolCalls[0].Success)
	assert.WithinDuration(t, time.Now(), bridge.toolCalls[0].Timestamp, 2*time.Second)
	assert.True(t, bridge.toolCalls[0].Duration >= 0)
}

func TestDelegateStreamBridge_RecordToolCall_Failure(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordToolCall("write_file", "input", "error: permission denied", false)

	require.Len(t, bridge.toolCalls, 1)
	assert.False(t, bridge.toolCalls[0].Success)
}

func TestDelegateStreamBridge_RecordToolCall_Multiple(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordToolCall("read_file", "input1", "output1", true)
	bridge.RecordToolCall("write_file", "input2", "output2", true)
	bridge.RecordToolCall("shell_command", "input3", "output3", false)

	require.Len(t, bridge.toolCalls, 3)
	assert.Equal(t, "read_file", bridge.toolCalls[0].ToolName)
	assert.Equal(t, "write_file", bridge.toolCalls[1].ToolName)
	assert.Equal(t, "shell_command", bridge.toolCalls[2].ToolName)
}

func TestDelegateStreamBridge_RecordFileChange(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordFileChange("foo.go")
	bridge.RecordFileChange("bar.go")
	bridge.RecordFileChange("baz.go")

	require.Len(t, bridge.filesChanged, 3)
	assert.Contains(t, bridge.filesChanged, "foo.go")
	assert.Contains(t, bridge.filesChanged, "bar.go")
	assert.Contains(t, bridge.filesChanged, "baz.go")
}

func TestDelegateStreamBridge_RecordFileChange_Deduplicates(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordFileChange("foo.go")
	bridge.RecordFileChange("bar.go")
	bridge.RecordFileChange("foo.go") // duplicate

	require.Len(t, bridge.filesChanged, 2)
	assert.Contains(t, bridge.filesChanged, "foo.go")
	assert.Contains(t, bridge.filesChanged, "bar.go")
}

func TestDelegateStreamBridge_RecordFileChange_EmptyPath(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordFileChange("")
	bridge.RecordFileChange("foo.go")

	require.Len(t, bridge.filesChanged, 2)
	assert.Contains(t, bridge.filesChanged, "")
	assert.Contains(t, bridge.filesChanged, "foo.go")
}

func TestDelegateStreamBridge_RecordFileChange_DeduplicatesEmptyPath(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordFileChange("")
	bridge.RecordFileChange("")

	require.Len(t, bridge.filesChanged, 1)
}

func TestDelegateStreamBridge_RecordTokenUsage(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordTokenUsage(100)
	assert.Equal(t, 100, bridge.tokenUsage)

	bridge.RecordTokenUsage(250)
	assert.Equal(t, 350, bridge.tokenUsage)

	bridge.RecordTokenUsage(0)
	assert.Equal(t, 350, bridge.tokenUsage)
}

func TestDelegateStreamBridge_RecordCost(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordCost(0.01)
	assert.Equal(t, 0.01, bridge.cost)

	bridge.RecordCost(0.02)
	assert.Equal(t, 0.03, bridge.cost)
}

func TestDelegateStreamBridge_RecordCost_Zero(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordCost(0)
	assert.Equal(t, 0.0, bridge.cost)
}

func TestDelegateStreamBridge_RecordIteration(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	assert.Equal(t, 0, bridge.iterations)

	bridge.RecordIteration()
	assert.Equal(t, 1, bridge.iterations)

	bridge.RecordIteration()
	assert.Equal(t, 2, bridge.iterations)

	bridge.RecordIteration()
	assert.Equal(t, 3, bridge.iterations)
}

func TestDelegateStreamBridge_GetResult(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordToolCall("read_file", "input", "output", true)
	bridge.RecordFileChange("foo.go")
	bridge.RecordFileChange("bar.go")
	bridge.RecordTokenUsage(500)
	bridge.RecordCost(0.05)
	bridge.RecordIteration()
	bridge.RecordIteration()

	result := bridge.GetResult("Completed successfully", "success", "")

	require.NotNil(t, result)
	assert.Equal(t, "Completed successfully", result.Summary)
	assert.Equal(t, "success", result.ExitStatus)
	assert.Empty(t, result.ErrorMessage)
	assert.Equal(t, 500, result.TokensUsed)
	assert.Equal(t, 0.05, result.Cost)
	assert.Equal(t, 2, result.Iterations)
	require.Len(t, result.ToolsCalled, 1)
	assert.Equal(t, "read_file", result.ToolsCalled[0].ToolName)
	require.Len(t, result.FilesChanged, 2)
	assert.Contains(t, result.FilesChanged, "foo.go")
	assert.Contains(t, result.FilesChanged, "bar.go")
}

func TestDelegateStreamBridge_GetResult_WithErrorMessage(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	result := bridge.GetResult("Partial completion", "error", "max iterations reached")

	require.NotNil(t, result)
	assert.Equal(t, "Partial completion", result.Summary)
	assert.Equal(t, "error", result.ExitStatus)
	assert.Equal(t, "max iterations reached", result.ErrorMessage)
}

func TestDelegateStreamBridge_GetResult_ReturnsSliceCopies(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	bridge.RecordToolCall("read_file", "input", "output", true)
	bridge.RecordFileChange("foo.go")

	result := bridge.GetResult("summary", "success", "")

	// Modify the returned slices and verify the bridge's internal slices are untouched
	result.ToolsCalled[0].ToolName = "tampered"
	result.FilesChanged[0] = "tampered"

	// Internal state should be unchanged
	assert.Equal(t, "read_file", bridge.toolCalls[0].ToolName)
	assert.Equal(t, "foo.go", bridge.filesChanged[0])
}

func TestDelegateStreamBridge_GetResult_Empty(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")

	result := bridge.GetResult("no work done", "success", "")

	require.NotNil(t, result)
	assert.Equal(t, "no work done", result.Summary)
	assert.Equal(t, "success", result.ExitStatus)
	assert.Empty(t, result.ErrorMessage)
	assert.Empty(t, result.ToolsCalled)
	assert.Empty(t, result.FilesChanged)
	assert.Equal(t, 0, result.TokensUsed)
	assert.Equal(t, 0.0, result.Cost)
	assert.Equal(t, 0, result.Iterations)
}

func TestDelegateStreamBridge_PublishActivity_WithEventBus(t *testing.T) {
	parent, bus := newTestAgentForStreamBridge(t)

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	bridge := NewDelegateStreamBridge(parent, "test-delegate-1")
	bridge.PublishActivity("started", "Starting task", 1)

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeDelegateActivity, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "test-delegate-1", data["delegate_id"])
		assert.Equal(t, "started", data["action"])
		assert.Equal(t, "Starting task", data["summary"])
		assert.Equal(t, 1, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delegate_activity event")
	}
}

func TestDelegateStreamBridge_PublishActivity_NilParent(t *testing.T) {
	bridge := NewDelegateStreamBridge(nil, "test-delegate-1")

	// Should not panic
	bridge.PublishActivity("started", "Starting task", 1)
}

func TestDelegateStreamBridge_PublishActivity_NilEventBus(t *testing.T) {
	parent := &Agent{} // eventBus is nil
	bridge := NewDelegateStreamBridge(parent, "test-delegate-1")

	// Should not panic
	bridge.PublishActivity("started", "Starting task", 1)
}

func TestDelegateStreamBridge_PublishActivity_Actions(t *testing.T) {
	parent, bus := newTestAgentForStreamBridge(t)

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	bridge := NewDelegateStreamBridge(parent, "test-delegate-2")

	bridge.PublishActivity("started", "Starting", 2)
	bridge.PublishActivity("tool_call", "Calling read_file", 2)
	bridge.PublishActivity("completed", "Done", 2)

	for i := 0; i < 3; i++ {
		select {
		case event := <-ch:
			assert.Equal(t, events.EventTypeDelegateActivity, event.Type)
			data := event.Data.(map[string]interface{})
			assert.Equal(t, "test-delegate-2", data["delegate_id"])
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for event %d", i+1)
		}
	}
}

func TestDelegateStreamBridge_PublishActivity_CompletedAction(t *testing.T) {
	parent, bus := newTestAgentForStreamBridge(t)

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	bridge := NewDelegateStreamBridge(parent, "comp-test")
	bridge.PublishActivity("completed", "All done", 1)

	select {
	case event := <-ch:
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "completed", data["action"])
		assert.Equal(t, "All done", data["summary"])
		assert.Equal(t, 1, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for completed event")
	}
}

func TestDelegateStreamBridge_PublishActivity_ErrorAction(t *testing.T) {
	parent, bus := newTestAgentForStreamBridge(t)

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	bridge := NewDelegateStreamBridge(parent, "err-test")
	bridge.PublishActivity("error", "Something went wrong", 2)

	select {
	case event := <-ch:
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "error", data["action"])
		assert.Equal(t, "Something went wrong", data["summary"])
		assert.Equal(t, 2, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for error event")
	}
}

func TestDelegateStreamBridge_GetResult_AfterStop(t *testing.T) {
	bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
	bridge.Start()

	bridge.RecordToolCall("read_file", "input", "output", true)
	bridge.RecordFileChange("foo.go")
	bridge.RecordTokenUsage(100)

	bridge.Stop()

	result := bridge.GetResult("summary", "success", "")

	require.NotNil(t, result)
	assert.Equal(t, "summary", result.Summary)
	assert.Equal(t, "success", result.ExitStatus)
	require.Len(t, result.ToolsCalled, 1)
	require.Len(t, result.FilesChanged, 1)
	assert.Equal(t, 100, result.TokensUsed)
}
