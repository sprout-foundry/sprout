package security

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
)

func TestNewApprovalManager(t *testing.T) {
	am := NewApprovalManager()
	assert.NotNil(t, am)
	assert.Equal(t, DefaultTimeout, am.timeout)
}

func TestApprovalManager_SetTimeout(t *testing.T) {
	am := NewApprovalManager()

	am.SetTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, am.timeout)

	// Zero resets to default
	am.SetTimeout(0)
	assert.Equal(t, DefaultTimeout, am.timeout)

	// Negative also resets to default
	am.SetTimeout(-1 * time.Second)
	assert.Equal(t, DefaultTimeout, am.timeout)
}

func TestApprovalManager_NilEventBus_ToolKind(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(1 * time.Second)

	// nil eventBus should return default for kind: false for tool kind
	result := am.RequestApproval(nil, ApprovalRequest{
		Kind: ApprovalKindTool,
	})
	assert.False(t, result, "nil event bus should reject tool requests")
}

func TestApprovalManager_NilEventBus_PromptKind(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(1 * time.Second)

	// nil eventBus should return default for kind
	result := am.RequestApproval(nil, ApprovalRequest{
		Kind:            ApprovalKindPrompt,
		DefaultResponse: true,
	})
	assert.True(t, result, "nil event bus should use DefaultResponse for prompt kind")
}

func TestApprovalManager_NilEventBus_PromptKindFalse(t *testing.T) {
	am := NewApprovalManager()

	result := am.RequestApproval(nil, ApprovalRequest{
		Kind:            ApprovalKindPrompt,
		DefaultResponse: false,
	})
	assert.False(t, result)
}

func TestApprovalManager_RespondToApproval_Nonexistent(t *testing.T) {
	am := NewApprovalManager()
	result := am.RespondToApproval("nonexistent-request-id", true)
	assert.False(t, result, "responding to nonexistent request should return false")
}

func TestApprovalManager_ToolApprovalFlow(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(2 * time.Second)
	eventBus := events.NewEventBus()

	// Subscribe to capture the actual request ID published by the manager.
	sub := eventBus.Subscribe("test-approval-flow")
	go func() {
		select {
		case evt := <-sub:
			data, ok := evt.Data.(map[string]interface{})
			if !ok {
				return
			}
			requestID, _ := data["request_id"].(string)
			if requestID != "" {
				am.RespondToApproval(requestID, true)
			}
		case <-time.After(2 * time.Second):
		}
	}()

	result := am.RequestToolApproval(eventBus, "client-1", "user-1", "shell_command", "dangerous", "test", nil)
	assert.True(t, result, "tool request should be approved via event bus response")
}

func TestApprovalManager_RequestApproval_Timeout(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(100 * time.Millisecond)
	eventBus := events.NewEventBus()

	start := time.Now()
	result := am.RequestApproval(eventBus, ApprovalRequest{
		Kind: ApprovalKindTool,
	})
	elapsed := time.Since(start)

	assert.False(t, result, "timeout should reject tool requests")
	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond, "should wait for timeout")
}

func TestApprovalManager_RespondToPrompt_Alias(t *testing.T) {
	am := NewApprovalManager()
	result := am.RespondToPrompt("nonexistent", true)
	assert.False(t, result)
}

func TestApprovalManager_SetApprovalTimeout_Alias(t *testing.T) {
	am := NewApprovalManager()
	am.SetApprovalTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, am.timeout)
}

func TestApprovalManager_SetPromptTimeout_Alias(t *testing.T) {
	am := NewApprovalManager()
	am.SetPromptTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, am.timeout)
}

func TestGlobalApprovalManager(t *testing.T) {
	am := NewApprovalManager()

	SetGlobalApprovalManager(am)
	retrieved := GetGlobalApprovalManager()
	assert.Equal(t, am, retrieved)
}

func TestGlobalApprovalManager_Nil(t *testing.T) {
	SetGlobalApprovalManager(nil)
	retrieved := GetGlobalApprovalManager()
	assert.Nil(t, retrieved)
}

func TestBackwardCompatibleAliases(t *testing.T) {
	am := NewApprovalManager()

	// SetGlobalPromptManager is alias for SetGlobalApprovalManager
	SetGlobalPromptManager(am)
	retrieved := GetGlobalPromptManager()
	assert.Equal(t, am, retrieved)
}

func TestApprovalManager_DefaultForKind(t *testing.T) {
	am := NewApprovalManager()

	// Tool kind always returns false
	assert.False(t, am.defaultForKind(ApprovalRequest{Kind: ApprovalKindTool}))

	// Prompt kind returns DefaultResponse
	assert.True(t, am.defaultForKind(ApprovalRequest{Kind: ApprovalKindPrompt, DefaultResponse: true}))
	assert.False(t, am.defaultForKind(ApprovalRequest{Kind: ApprovalKindPrompt, DefaultResponse: false}))

	// Unknown kind returns false
	assert.False(t, am.defaultForKind(ApprovalRequest{Kind: ApprovalKind(99)}))
}

func TestApprovalManager_GenerateRequestID(t *testing.T) {
	am := NewApprovalManager()

	toolID := am.generateRequestID(ApprovalKindTool)
	assert.Contains(t, toolID, "sec_")

	promptID := am.generateRequestID(ApprovalKindPrompt)
	assert.Contains(t, promptID, "sec_prompt_")

	unknownID := am.generateRequestID(ApprovalKind(99))
	assert.Contains(t, unknownID, "sec_")
}

func TestApprovalManager_GenerateRequestID_Unique(t *testing.T) {
	am := NewApprovalManager()
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := am.generateRequestID(ApprovalKindTool)
		assert.False(t, ids[id], "generated duplicate ID: %s", id)
		ids[id] = true
	}
}

func TestApprovalKind_Constants(t *testing.T) {
	assert.Equal(t, ApprovalKind(0), ApprovalKindTool)
	assert.Equal(t, ApprovalKind(1), ApprovalKindPrompt)
}

func TestApprovalManager_RequestPrompt(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(100 * time.Millisecond)
	eventBus := events.NewEventBus()

	result := am.RequestPrompt(eventBus, "user-1", "Allow this?", true, nil)
	assert.True(t, result, "timeout should use DefaultResponse=true")
}

func TestApprovalManager_RequestPrompt_DefaultFalse(t *testing.T) {
	am := NewApprovalManager()
	am.SetTimeout(100 * time.Millisecond)
	eventBus := events.NewEventBus()

	result := am.RequestPrompt(eventBus, "user-1", "Allow this?", false, nil)
	assert.False(t, result, "timeout should use DefaultResponse=false")
}
