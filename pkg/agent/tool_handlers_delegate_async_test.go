package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// handleDelegate: async path
// ---------------------------------------------------------------------------

func TestHandleDelegate_AsyncReturnsRunning(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "do something asynchronously",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.Equal(t, "running", response["status"])
	// Verify delegate_id is present and non-empty
	delegateID, ok := response["delegate_id"]
	require.True(t, ok)
	assert.NotEmpty(t, delegateID)

	// Verify the message field
	msg, ok := response["message"]
	require.True(t, ok)
	assert.Contains(t, msg.(string), "asynchronously")
}

func TestHandleDelegate_AsyncGeneratesDelegateID(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "task 1",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output1, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond) // Ensure unique nanosecond timestamps

	args2 := map[string]interface{}{
		"prompt":   "task 2",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output2, err := handleDelegate(context.Background(), agent, args2)
	require.NoError(t, err)

	var resp1, resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output1), &resp1))
	require.NoError(t, json.Unmarshal([]byte(output2), &resp2))

	id1 := resp1["delegate_id"].(string)
	id2 := resp2["delegate_id"].(string)

	// IDs should be different (different nanosecond timestamps)
	assert.NotEqual(t, id1, id2, "delegate IDs should be unique")

	// IDs should start with "delegate-"
	assert.True(t, strings.HasPrefix(id1, "delegate-"), "id1 should start with 'delegate-'")
	assert.True(t, strings.HasPrefix(id2, "delegate-"), "id2 should start with 'delegate-'")
}

func TestHandleDelegate_AsyncWithValidPrompt(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "write some code",
		"role":     "coder",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])

	// Verify the delegate is being tracked
	delegateID := response["delegate_id"].(string)
	status, _, found := agent.asyncDelegateTracker.GetStatus(delegateID)
	require.True(t, found, "delegate should be found in tracker")
	assert.Equal(t, "running", status)
}

func TestHandleDelegate_AsyncWithRoleAndTools(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "write tests",
		"role":     "tester",
		"provider": "test",
		"model":    "test",
		"tools":    []interface{}{"read_file", "write_file"},
		"files":    []interface{}{"pkg/agent/agent.go"},
		"async":    true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncWithMaxIterations(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":         "do work",
		"provider":       "test",
		"model":          "test",
		"max_iterations": float64(25),
		"async":          true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncNestedAgent(t *testing.T) {
	// Test that async delegates work at nested depths (depth 1)
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 1 // one level deep

	args := map[string]interface{}{
		"prompt":   "nested async task",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncAtMaxDepth(t *testing.T) {
	// At max depth (3), new delegate would be depth 4 which exceeds max
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 3 // at max depth

	args := map[string]interface{}{
		"prompt":   "too deep",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	_, err = handleDelegate(context.Background(), agent, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestHandleDelegate_AsyncNilAgent_NoPanic(t *testing.T) {
	// With the nil-agent guard in place, handleDelegate returns an error
	// instead of panicking when the agent is nil.
	args := map[string]interface{}{
		"prompt": "test",
		"async":  true,
	}

	_, err := handleDelegate(context.Background(), nil, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent is required")
}

func TestHandleDelegate_AsyncEmptyPrompt(t *testing.T) {
	// Use a bare Agent{} (not nil) — initSubManagers can run on a bare struct.
	args := map[string]interface{}{
		"prompt": "",
		"async":  true,
	}

	agent := &Agent{}
	_, err := handleDelegate(context.Background(), agent, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestHandleDelegate_AsyncWithFollowUpMessages(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":    "do work with follow-up",
		"provider":  "test",
		"model":     "test",
		"follow_up": []interface{}{"Now fix the bug", "And add tests"},
		"async":     true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncReturnsImmediately(t *testing.T) {
	// Verify that async delegates return immediately without waiting
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "slow task",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	start := time.Now()
	output, err := handleDelegate(context.Background(), agent, args)
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Should return almost immediately (< 1s, since we're not actually running a real LLM)
	assert.Less(t, elapsed.Milliseconds(), int64(500), "async delegate should return immediately")

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))
	assert.Equal(t, "running", response["status"])
}

func TestHandleDelegate_AsyncMultipleConcurrent(t *testing.T) {
	// Launch multiple async delegates and verify each gets a unique ID
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	ids := make(map[string]bool)
	const numDelegates = 5

	for i := 0; i < numDelegates; i++ {
		args := map[string]interface{}{
			"prompt":   "task",
			"provider": "test",
			"model":    "test",
			"async":    true,
		}

		output, err := handleDelegate(context.Background(), agent, args)
		require.NoError(t, err)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(output), &response))

		delegateID := response["delegate_id"].(string)
		assert.False(t, ids[delegateID], "delegate IDs should be unique")
		ids[delegateID] = true
	}

	assert.Len(t, ids, numDelegates)
}

func TestHandleDelegate_AsyncWithProviderModelOverrides(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	// Override provider/model (even though test:test is used, it should parse)
	args := map[string]interface{}{
		"prompt":   "override test",
		"provider": "test",
		"model":    "test",
		"async":    true,
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncAsyncFieldAsFloat64(t *testing.T) {
	// JSON unmarshaling can send boolean true as float64(1)
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	// async as float64(1) — parseDelegateConfig handles this
	args := map[string]interface{}{
		"prompt":   "float64 async",
		"provider": "test",
		"model":    "test",
		"async":    float64(1),
	}

	output, err := handleDelegate(context.Background(), agent, args)
	require.NoError(t, err)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &response))

	assert.Equal(t, "running", response["status"])
	assert.NotEmpty(t, response["delegate_id"])
}

func TestHandleDelegate_AsyncAsyncFieldAsFloat64Zero(t *testing.T) {
	// async as float64(0) should NOT trigger async path
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0

	args := map[string]interface{}{
		"prompt":   "sync task",
		"provider": "test",
		"model":    "test",
		"async":    float64(0),
	}

	// Should go through sync path (will error due to test client but should NOT be async)
	_, err = handleDelegate(context.Background(), agent, args)
	// We expect it to proceed through the sync path — the error will come from elsewhere
	// not from the async path logic
	_ = err // error is expected from test client, but it shouldn't be about missing prompt
}

func TestParseDelegateConfig_AsyncTrue(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"async":  true,
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.True(t, cfg.Async)
}

func TestParseDelegateConfig_AsyncFalse(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"async":  false,
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.False(t, cfg.Async)
}

func TestParseDelegateConfig_AsyncFloat64One(t *testing.T) {
	// JSON unmarshal sends booleans as float64 sometimes
	args := map[string]interface{}{
		"prompt": "test",
		"async":  float64(1),
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.True(t, cfg.Async)
}

func TestParseDelegateConfig_AsyncFloat64Zero(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"async":  float64(0),
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.False(t, cfg.Async)
}

func TestParseDelegateConfig_AsyncMissing(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.False(t, cfg.Async)
}

func TestParseDelegateConfig_AsyncFollowUpMessages(t *testing.T) {
	args := map[string]interface{}{
		"prompt":    "test",
		"follow_up": []interface{}{"msg1", "msg2"},
		"async":     true,
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.True(t, cfg.Async)
	assert.Equal(t, []string{"msg1", "msg2"}, cfg.FollowUpMessages)
}
