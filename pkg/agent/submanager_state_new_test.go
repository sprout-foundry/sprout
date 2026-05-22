package agent

import (
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestNewAgentStateManager_Defaults(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Messages should be empty slice, not nil
	msgs := sm.GetMessages()
	if msgs == nil {
		t.Error("messages should not be nil")
	}
	if len(msgs) != 0 {
		t.Errorf("messages should be empty, got %d", len(msgs))
	}

	// Default persona
	if sm.GetActivePersona() != "orchestrator" {
		t.Errorf("default persona = %q, want orchestrator", sm.GetActivePersona())
	}

	// Default history index
	if sm.GetHistoryIndex() != -1 {
		t.Errorf("default history index = %d, want -1", sm.GetHistoryIndex())
	}

	// Default false stop detection enabled
	if !sm.IsFalseStopDetectionEnabled() {
		t.Error("false stop detection should be enabled by default")
	}

	// Default cost
	if sm.GetTotalCost() != 0 {
		t.Errorf("default cost = %f, want 0", sm.GetTotalCost())
	}

	// Default tokens
	if sm.GetTotalTokens() != 0 {
		t.Errorf("default tokens = %d, want 0", sm.GetTotalTokens())
	}

	// Default LLM call count
	if sm.GetLLMCallCount() != 0 {
		t.Errorf("default LLM call count = %d, want 0", sm.GetLLMCallCount())
	}

	// Should have optimizer
	if sm.GetOptimizer() == nil {
		t.Error("should have a default optimizer")
	}

	// Should have conversation pruner
	if sm.GetConversationPruner() == nil {
		t.Error("should have a default conversation pruner")
	}

	// Should have circuit breaker
	if sm.GetCircuitBreaker() == nil {
		t.Error("should have a default circuit breaker")
	}

	// Should have empty command history
	cmds := sm.GetCommandHistory()
	if cmds == nil {
		t.Error("command history should not be nil")
	}
	if len(cmds) != 0 {
		t.Errorf("command history should be empty, got %d", len(cmds))
	}
}

func TestAgentStateManager_Messages(t *testing.T) {
	sm := NewAgentStateManager(false)

	// SetMessages
	msgs := []api.Message{{Role: "user", Content: "hello"}}
	sm.SetMessages(msgs)
	if got := sm.GetMessages(); len(got) != 1 {
		t.Errorf("GetMessages = %d, want 1", len(got))
	}

	// AddMessage
	sm.AddMessage(api.Message{Role: "assistant", Content: "world"})
	if got := sm.GetMessages(); len(got) != 2 {
		t.Errorf("after AddMessage, GetMessages = %d, want 2", len(got))
	}
	if sm.GetMessages()[1].Role != "assistant" {
		t.Error("second message should be assistant")
	}
}

func TestAgentStateManager_Session(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetSessionID("sess-123")
	if sm.GetSessionID() != "sess-123" {
		t.Errorf("GetSessionID = %q, want sess-123", sm.GetSessionID())
	}

	sm.SetSessionID("")
	if sm.GetSessionID() != "" {
		t.Errorf("GetSessionID = %q, want empty", sm.GetSessionID())
	}
}

func TestAgentStateManager_TurnCheckpoints(t *testing.T) {
	sm := NewAgentStateManager(false)

	cp := TurnCheckpoint{StartIndex: 0, EndIndex: 5, Summary: "checkpoint 1"}
	sm.AddTurnCheckpoint(cp)

	checkpoints := sm.GetTurnCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("GetTurnCheckpoints = %d, want 1", len(checkpoints))
	}

	sm.SetTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 6, EndIndex: 10, Summary: "checkpoint 2"},
		{StartIndex: 11, EndIndex: 15, Summary: "checkpoint 3"},
	})
	checkpoints = sm.GetTurnCheckpoints()
	if len(checkpoints) != 2 {
		t.Errorf("after SetTurnCheckpoints, got %d, want 2", len(checkpoints))
	}
}

func TestAgentStateManager_CheckpointMutex(t *testing.T) {
	sm := NewAgentStateManager(false)
	mu := sm.GetCheckpointMutex()
	if mu == nil {
		t.Error("GetCheckpointMutex should not be nil")
	}
}

func TestAgentStateManager_PreviousSummary(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetPreviousSummary("Summary of previous work")
	if got := sm.GetPreviousSummary(); got != "Summary of previous work" {
		t.Errorf("GetPreviousSummary = %q, want correct value", got)
	}
}

func TestAgentStateManager_Optimizer(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Default optimizer should exist
	if sm.GetOptimizer() == nil {
		t.Error("should have default optimizer")
	}

	// Replace optimizer
	newOpt := NewConversationOptimizer(false, false)
	sm.SetOptimizer(newOpt)
	if sm.GetOptimizer() != newOpt {
		t.Error("SetOptimizer should replace optimizer")
	}
}

func TestAgentStateManager_ContextTokens(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetCurrentContextTokens(5000)
	sm.SetMaxContextTokens(100000)

	if got := sm.GetCurrentContextTokens(); got != 5000 {
		t.Errorf("GetCurrentContextTokens = %d, want 5000", got)
	}
	if got := sm.GetMaxContextTokens(); got != 100000 {
		t.Errorf("GetMaxContextTokens = %d, want 100000", got)
	}
}

func TestAgentStateManager_ContextWarning(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.IsContextWarningIssued() {
		t.Error("default should be false")
	}

	sm.SetContextWarningIssued(true)
	if !sm.IsContextWarningIssued() {
		t.Error("should return true after setting")
	}
}

func TestAgentStateManager_TaskActions(t *testing.T) {
	sm := NewAgentStateManager(false)

	action := TaskAction{Type: "file_read", Description: "read file", Details: "file.go"}
	sm.AddTaskAction(action)

	actions := sm.GetTaskActions()
	if len(actions) != 1 {
		t.Errorf("GetTaskActions = %d, want 1", len(actions))
	}

	sm.SetTaskActions([]TaskAction{{Type: "file_write", Description: "write file", Details: "file2.go"}})
	actions = sm.GetTaskActions()
	if len(actions) != 1 || actions[0].Type != "file_write" {
		t.Error("SetTaskActions should replace actions")
	}
}

func TestAgentStateManager_TaskActionsMutex(t *testing.T) {
	sm := NewAgentStateManager(false)
	mu := sm.GetTaskActionsMutex()
	if mu == nil {
		t.Error("GetTaskActionsMutex should not be nil")
	}
}

func TestAgentStateManager_Cost(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetTotalCost(10.5)
	if got := sm.GetTotalCost(); got != 10.5 {
		t.Errorf("GetTotalCost = %f, want 10.5", got)
	}

	sm.AddCost(5.0)
	if got := sm.GetTotalCost(); got != 15.5 {
		t.Errorf("after AddCost(5), GetTotalCost = %f, want 15.5", got)
	}

	sm.AddCost(-3.0)
	if got := sm.GetTotalCost(); got != 12.5 {
		t.Errorf("after AddCost(-3), GetTotalCost = %f, want 12.5", got)
	}
}

func TestAgentStateManager_TokenCounts(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetTotalTokens(1000)
	sm.SetPromptTokens(700)
	sm.SetCompletionTokens(300)

	if sm.GetTotalTokens() != 1000 {
		t.Errorf("GetTotalTokens = %d, want 1000", sm.GetTotalTokens())
	}
	if sm.GetPromptTokens() != 700 {
		t.Errorf("GetPromptTokens = %d, want 700", sm.GetPromptTokens())
	}
	if sm.GetCompletionTokens() != 300 {
		t.Errorf("GetCompletionTokens = %d, want 300", sm.GetCompletionTokens())
	}
}

func TestAgentStateManager_LLMCallCount(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.GetLLMCallCount() != 0 {
		t.Errorf("default LLM call count = %d, want 0", sm.GetLLMCallCount())
	}

	sm.SetLLMCallCount(5)
	if sm.GetLLMCallCount() != 5 {
		t.Errorf("GetLLMCallCount = %d, want 5", sm.GetLLMCallCount())
	}

	sm.IncrementLLMCallCount()
	if sm.GetLLMCallCount() != 6 {
		t.Errorf("after IncrementLLMCallCount = %d, want 6", sm.GetLLMCallCount())
	}

	sm.IncrementLLMCallCount()
	sm.IncrementLLMCallCount()
	if sm.GetLLMCallCount() != 8 {
		t.Errorf("after two increments = %d, want 8", sm.GetLLMCallCount())
	}
}

func TestAgentStateManager_EstimatedTokenResponses(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetEstimatedTokenResponses(2000)
	if got := sm.GetEstimatedTokenResponses(); got != 2000 {
		t.Errorf("GetEstimatedTokenResponses = %d, want 2000", got)
	}
}

func TestAgentStateManager_CacheStats(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetCachedTokens(500)
	sm.SetCachedCostSavings(1.25)

	if sm.GetCachedTokens() != 500 {
		t.Errorf("GetCachedTokens = %d, want 500", sm.GetCachedTokens())
	}
	if sm.GetCachedCostSavings() != 1.25 {
		t.Errorf("GetCachedCostSavings = %f, want 1.25", sm.GetCachedCostSavings())
	}
}

func TestAgentStateManager_SkillsAndPersona(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetActiveSkills([]string{"project-planning", "browse-debugging"})
	skills := sm.GetActiveSkills()
	if len(skills) != 2 {
		t.Errorf("GetActiveSkills = %d, want 2", len(skills))
	}

	sm.SetActivePersona("coder")
	if sm.GetActivePersona() != "coder" {
		t.Errorf("GetActivePersona = %q, want coder", sm.GetActivePersona())
	}
}

func TestAgentStateManager_CircuitBreaker(t *testing.T) {
	sm := NewAgentStateManager(false)

	cb := sm.GetCircuitBreaker()
	if cb == nil {
		t.Error("should have default circuit breaker")
	}

	newCB := &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)}
	sm.SetCircuitBreaker(newCB)
	if sm.GetCircuitBreaker() != newCB {
		t.Error("SetCircuitBreaker should replace circuit breaker")
	}
}

func TestAgentStateManager_ToolCallGuidance(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.IsToolCallGuidanceAdded() {
		t.Error("default should be false")
	}

	sm.SetToolCallGuidanceAdded(true)
	if !sm.IsToolCallGuidanceAdded() {
		t.Error("should return true after setting")
	}
}

func TestAgentStateManager_PendingState(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetPendingSwitchContextRefresh("refresh-val")
	if sm.GetPendingSwitchContextRefresh() != "refresh-val" {
		t.Errorf("GetPendingSwitchContextRefresh = %q, want refresh-val", sm.GetPendingSwitchContextRefresh())
	}

	sm.SetPendingStrictSwitchNotice("strict-notice")
	if sm.GetPendingStrictSwitchNotice() != "strict-notice" {
		t.Errorf("GetPendingStrictSwitchNotice = %q, want strict-notice", sm.GetPendingStrictSwitchNotice())
	}

	sm.SetPendingSystemSupplement("supplement")
	if sm.GetPendingSystemSupplement() != "supplement" {
		t.Errorf("GetPendingSystemSupplement = %q, want supplement", sm.GetPendingSystemSupplement())
	}
}

func TestAgentStateManager_FalseStopDetection(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Default enabled
	if !sm.IsFalseStopDetectionEnabled() {
		t.Error("should be enabled by default")
	}

	sm.SetFalseStopDetectionEnabled(false)
	if sm.IsFalseStopDetectionEnabled() {
		t.Error("should return false after disabling")
	}
}

func TestAgentStateManager_Termination(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetLastRunTerminationReason("context_limit")
	if got := sm.GetLastRunTerminationReason(); got != "context_limit" {
		t.Errorf("GetLastRunTerminationReason = %q, want context_limit", got)
	}
}

func TestAgentStateManager_CommandHistory(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetCommandHistory([]string{"cmd1", "cmd2"})
	history := sm.GetCommandHistory()
	if len(history) != 2 {
		t.Errorf("GetCommandHistory = %d, want 2", len(history))
	}

	sm.SetHistoryIndex(1)
	if sm.GetHistoryIndex() != 1 {
		t.Errorf("GetHistoryIndex = %d, want 1", sm.GetHistoryIndex())
	}

	mu := sm.GetHistoryMutex()
	if mu == nil {
		t.Error("GetHistoryMutex should not be nil")
	}
}

func TestAgentStateManager_Pause(t *testing.T) {
	sm := NewAgentStateManager(false)

	ps := &PauseState{OriginalTask: "test pause"}
	sm.SetPauseState(ps)
	if sm.GetPauseState() != ps {
		t.Error("SetPauseState should set the pause state")
	}

	mu := sm.GetPauseMutex()
	if mu == nil {
		t.Error("GetPauseMutex should not be nil")
	}
}

func TestAgentStateManager_Tracing(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.GetTraceSession() != nil {
		t.Error("default trace session should be nil")
	}

	sm.SetTraceSession("trace-id-123")
	if sm.GetTraceSession() != "trace-id-123" {
		t.Errorf("GetTraceSession = %v, want trace-id-123", sm.GetTraceSession())
	}
}

func TestAgentStateManager_SessionConfig(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetSessionProvider("zai")
	if sm.GetSessionProvider() != "zai" {
		t.Errorf("GetSessionProvider = %s, want zai", sm.GetSessionProvider())
	}

	sm.SetSessionModel("claude-sonnet-4")
	if sm.GetSessionModel() != "claude-sonnet-4" {
		t.Errorf("GetSessionModel = %q, want claude-sonnet-4", sm.GetSessionModel())
	}
}

func TestAgentStateManager_ConfigOverrides(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.GetConfigOverrides() != nil {
		t.Error("default config overrides should be nil")
	}

	overrides := map[string]interface{}{"key": "value"}
	sm.SetConfigOverrides(overrides)
	got := sm.GetConfigOverrides()
	if got == nil || len(got) != 1 || got["key"] != "value" {
		t.Error("SetConfigOverrides should set overrides")
	}
}

func TestAgentStateManager_CurrentIteration(t *testing.T) {
	sm := NewAgentStateManager(false)

	if sm.GetCurrentIteration() != 0 {
		t.Errorf("GetCurrentIteration = %d, want 0", sm.GetCurrentIteration())
	}

	sm.SetCurrentIteration(5)
	if sm.GetCurrentIteration() != 5 {
		t.Errorf("GetCurrenIteration = %d, want 5", sm.GetCurrentIteration())
	}
}

func TestAgentStateManager_ConversationPruner(t *testing.T) {
	sm := NewAgentStateManager(false)

	pruner := sm.GetConversationPruner()
	if pruner == nil {
		t.Error("should have default conversation pruner")
	}
}

func TestAgentStateManager_ConcurrentAccess(t *testing.T) {
	sm := NewAgentStateManager(false)

	var wg sync.WaitGroup

	// Concurrent writes to different fields
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sm.SetCurrentContextTokens(n)
			sm.SetTotalTokens(n)
			sm.SetTotalCost(float64(n))
			sm.SetCurrentIteration(n)
			sm.SetPromptTokens(n)
			sm.SetCompletionTokens(n)
			sm.IncrementLLMCallCount()
			sm.AddCost(0.01)
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Concurrent reads should not panic
			sm.GetCurrentContextTokens()
			sm.GetTotalTokens()
			sm.GetTotalCost()
			sm.GetCurrentIteration()
			sm.GetPromptTokens()
			sm.GetCompletionTokens()
			sm.GetLLMCallCount()
			sm.GetMessages()
		}()
	}

	wg.Wait()
}

func TestAgentStateManager_ConcurrentMessages(t *testing.T) {
	sm := NewAgentStateManager(false)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sm.AddMessage(api.Message{Role: "user", Content: string(rune('a' + n%26))})
		}(i)
	}
	wg.Wait()

	// All messages should be present
	if len(sm.GetMessages()) != 50 {
		t.Errorf("expected 50 messages, got %d", len(sm.GetMessages()))
	}
}
