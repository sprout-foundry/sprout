package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// handleDelegateStatus: error cases
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_NilAgent(t *testing.T) {
	_, err := handleDelegateStatus(context.Background(), nil, map[string]interface{}{
		"delegate_id": "some-id",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent is required")
}

func TestHandleDelegateStatus_EmptyDelegateID(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	_, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delegate_id is required")
}

func TestHandleDelegateStatus_MissingDelegateID(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	_, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"some_other_key": "value",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delegate_id is required")
}

func TestHandleDelegateStatus_NotFound(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	_, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "nonexistent-id",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: running delegate
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_Running(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	// Start a slow delegate
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("running-del", DelegateConfig{Prompt: "slow task"}, nil, runFn))
	time.Sleep(10 * time.Millisecond) // let it start

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "running-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running-del", response["delegate_id"])
	assert.Equal(t, "running", response["status"])
	assert.Equal(t, "Delegate is still running", response["message"])
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: completed delegate
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_Completed(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	// Start a fast delegate that completes
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{
			Summary:      "tests written successfully",
			ExitStatus:   "success",
			TokensUsed:   500,
			Cost:         0.025,
			Iterations:   5,
			FilesChanged: []string{"test1.go", "test2.go"},
			ToolsCalled:  []ToolCallRecord{{ToolName: "write_file", Success: true}},
		}, nil
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("completed-del", DelegateConfig{Prompt: "test"}, nil, runFn))
	time.Sleep(50 * time.Millisecond) // let it complete

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "completed-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "completed-del", response["delegate_id"])
	assert.Equal(t, "completed", response["status"])
	assert.Equal(t, "tests written successfully", response["summary"])
	assert.Equal(t, "success", response["exit_status"])
	assert.Equal(t, float64(500), response["tokens_used"])
	assert.Equal(t, float64(0.025), response["cost"])
	assert.Equal(t, float64(5), response["iterations"])
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: failed delegate
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_Failed(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	// Start a delegate that fails
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("something went wrong")
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("failed-del", DelegateConfig{Prompt: "fail"}, nil, runFn))
	time.Sleep(50 * time.Millisecond) // let it fail

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "failed-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "failed-del", response["delegate_id"])
	assert.Equal(t, "failed", response["status"])
	assert.Equal(t, "Delegate failed: something went wrong", response["summary"])
	assert.Equal(t, "error", response["exit_status"])
	assert.Equal(t, "something went wrong", response["error"])
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: nil result (edge case)
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_Completed_NilResult(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	// Manually inject an entry with completed status but nil result
	// This is an edge case: status is set but result wasn't properly stored
	entry := &asyncDelegateEntry{
		ID:        "nil-result-del",
		Config:    DelegateConfig{Prompt: "test"},
		Status:    "completed",
		Result:    nil,
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
		Cancel:    func() {},
	}

	tracker.mu.Lock()
	tracker.entries["nil-result-del"] = entry
	tracker.mu.Unlock()

	agent := &Agent{}
	agent.initSubManagers()
	// Replace tracker with our test tracker
	agent.asyncDelegateTracker = tracker

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "nil-result-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "nil-result-del", response["delegate_id"])
	assert.Equal(t, "completed", response["status"])
	// With nil result, the response should not contain summary/exit_status/etc.
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: result with files_changed and tools_called
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_Completed_WithFilesAndTools(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{
			Summary:    "refactored code",
			ExitStatus: "success",
			FilesChanged: []string{"pkg/agent/foo.go", "pkg/agent/bar.go"},
			ToolsCalled: []ToolCallRecord{
				{ToolName: "read_file", Input: "foo.go", Success: true},
				{ToolName: "edit_file", Input: "bar.go", Success: true},
				{ToolName: "shell_command", Input: "go test", Success: true},
			},
			TokensUsed: 1000,
			Cost:       0.05,
			Iterations: 10,
		}, nil
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("full-del", DelegateConfig{Prompt: "refactor"}, nil, runFn))
	time.Sleep(50 * time.Millisecond)

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "full-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "full-del", response["delegate_id"])
	assert.Equal(t, "completed", response["status"])

	// Verify files_changed is present
	files, ok := response["files_changed"]
	require.True(t, ok)
	filesArr := files.([]interface{})
	require.Len(t, filesArr, 2)

	// Verify tools_called is present
	tools, ok := response["tool_calls"]
	require.True(t, ok)
	toolsArr := tools.([]interface{})
	require.Len(t, toolsArr, 3)
}

func TestHandleDelegateStatus_Completed_ErrorMessagePresent(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("specific error: file not found")
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("err-del", DelegateConfig{Prompt: "fail"}, nil, runFn))
	time.Sleep(50 * time.Millisecond)

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "err-del",
	})
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "failed", response["status"])
	assert.Contains(t, response["error"].(string), "file not found")
}

// ---------------------------------------------------------------------------
// handleDelegateStatus: output is valid JSON
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_OutputIsValidJSON(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{
			Summary:    "done",
			ExitStatus: "success",
		}, nil
	}

	require.NoError(t, agent.asyncDelegateTracker.Start("json-del", DelegateConfig{Prompt: "test"}, nil, runFn))
	time.Sleep(50 * time.Millisecond)

	output, err := handleDelegateStatus(context.Background(), agent, map[string]interface{}{
		"delegate_id": "json-del",
	})
	require.NoError(t, err)

	// Verify it's valid JSON
	var raw map[string]interface{}
	err = json.Unmarshal([]byte(output), &raw)
	require.NoError(t, err)

	// Verify all expected fields are present
	assert.Contains(t, raw, "delegate_id")
	assert.Contains(t, raw, "status")
}
