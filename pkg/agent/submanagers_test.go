package agent

import (
	"errors"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// === AgentMCPManager ===

func TestNewAgentMCPManagerDefaults(t *testing.T) {
	t.Parallel()

	m := NewAgentMCPManager()

	if m.GetManager() == nil {
		t.Error("GetManager() is nil; want non-nil")
	}
	if m.GetToolsCache() != nil {
		t.Error("GetToolsCache() is non-nil; want nil")
	}
	if m.IsInitialized() {
		t.Error("IsInitialized() is true; want false")
	}
	if m.GetInitError() != nil {
		t.Error("GetInitError() is non-nil; want nil")
	}
}

func TestAgentMCPManagerGettersSetters(t *testing.T) {
	t.Parallel()

	m := NewAgentMCPManager()

	// SetManager
	m.SetManager(nil)
	if m.GetManager() != nil {
		t.Error("SetManager(nil) then GetManager() should be nil")
	}

	// SetToolsCache
	var tool api.Tool
	tool.Function.Name = "test"
	m.SetToolsCache([]api.Tool{tool})
	if len(m.GetToolsCache()) != 1 {
		t.Error("SetToolsCache then GetToolsCache should return same length slice")
	}

	// SetInitialized
	m.SetInitialized(true)
	if !m.IsInitialized() {
		t.Error("SetInitialized(true) then IsInitialized() should be true")
	}
	m.SetInitialized(false)
	if m.IsInitialized() {
		t.Error("SetInitialized(false) then IsInitialized() should be false")
	}

	// SetInitError: init failed
	someErr := errors.New("init failed")
	m.SetInitError(someErr)
	if m.GetInitError() != someErr {
		t.Error("SetInitError then GetInitError should return same error")
	}
	m.SetInitError(nil)
	if m.GetInitError() != nil {
		t.Error("SetInitError(nil) then GetInitError() should be nil")
	}

	// LockInit/UnlockInit — just verify no deadlock
	m.LockInit()
	m.UnlockInit()
}

// === AgentOutputManager ===

func TestNewAgentOutputManagerDefaults(t *testing.T) {
	t.Parallel()

	m := NewAgentOutputManager()

	if m.IsStreamingEnabled() {
		t.Error("IsStreamingEnabled() is true; want false")
	}
	if m.GetStreamingCallback() != nil {
		t.Error("GetStreamingCallback() is non-nil; want nil")
	}
	if m.GetReasoningCallback() != nil {
		t.Error("GetReasoningCallback() is non-nil; want nil")
	}
	if m.GetFlushCallback() != nil {
		t.Error("GetFlushCallback() is non-nil; want nil")
	}
	if m.GetOutputMutex() != nil {
		t.Error("GetOutputMutex() is non-nil; want nil")
	}
	if m.GetOutputRouter() != nil {
		t.Error("GetOutputRouter() is non-nil; want nil")
	}
	if m.GetAsyncOutput() != nil {
		t.Error("GetAsyncOutput() is non-nil; want nil")
	}
	if m.GetAsyncBufferSize() != 0 {
		t.Errorf("GetAsyncBufferSize() is %d; want 0", m.GetAsyncBufferSize())
	}

	// eventMetadata should be initialized (not nil)
	meta := m.GetEventMetadata()
	if meta == nil {
		t.Error("GetEventMetadata() is nil; want non-nil initialized map")
	}
}

func TestAgentOutputManagerGettersSetters(t *testing.T) {
	t.Parallel()

	m := NewAgentOutputManager()

	// StreamingEnabled
	m.SetStreamingEnabled(true)
	if !m.IsStreamingEnabled() {
		t.Error("SetStreamingEnabled(true) then IsStreamingEnabled() should be true")
	}
	m.SetStreamingEnabled(false)
	if m.IsStreamingEnabled() {
		t.Error("SetStreamingEnabled(false) then IsStreamingEnabled() should be false")
	}

	// StreamingCallback
	var streamingCalled bool
	streamingCb := func(s string) { streamingCalled = true }
	m.SetStreamingCallback(streamingCb)
	streamingCalled = false
	m.GetStreamingCallback()("test")
	if !streamingCalled {
		t.Error("SetStreamingCallback should store the callback")
	}

	// ReasoningCallback
	var reasoningCalled bool
	reasoningCb := func(s string) { reasoningCalled = true }
	m.SetReasoningCallback(reasoningCb)
	reasoningCalled = false
	m.GetReasoningCallback()("test")
	if !reasoningCalled {
		t.Error("SetReasoningCallback should store the callback")
	}

	// FlushCallback
	var flushCalled bool
	flushCb := func() { flushCalled = true }
	m.SetFlushCallback(flushCb)
	flushCalled = false
	m.GetFlushCallback()()
	if !flushCalled {
		t.Error("SetFlushCallback should store the callback")
	}

	// OutputMutex
	mu := &sync.Mutex{}
	m.SetOutputMutex(mu)
	if m.GetOutputMutex() != mu {
		t.Error("SetOutputMutex then GetOutputMutex should return same mutex")
	}

	// OutputRouter
	router := &OutputRouter{}
	m.SetOutputRouter(router)
	if m.GetOutputRouter() != router {
		t.Error("SetOutputRouter then GetOutputRouter should return same router")
	}

	// AsyncOutput
	ch := make(chan string, 1)
	m.SetAsyncOutput(ch)
	if m.GetAsyncOutput() != ch {
		t.Error("SetAsyncOutput then GetAsyncOutput should return same channel")
	}

	// AsyncBufferSize
	m.SetAsyncBufferSize(50)
	if m.GetAsyncBufferSize() != 50 {
		t.Errorf("SetAsyncBufferSize(50) then GetAsyncBufferSize() should be 50; got %d", m.GetAsyncBufferSize())
	}

	// StreamingBuffer / ReasoningBuffer
	sb := m.GetStreamingBuffer()
	if sb == nil {
		t.Error("GetStreamingBuffer() should not be nil")
	}
	rb := m.GetReasoningBuffer()
	if rb == nil {
		t.Error("GetReasoningBuffer() should not be nil")
	}

	// EventMetadata
	meta := map[string]interface{}{"key": "value"}
	m.SetEventMetadata(meta)
	got := m.GetEventMetadata()
	if got["key"] != "value" {
		t.Error("SetEventMetadata then GetEventMetadata should preserve values")
	}

	// GetEventMetadataMutex
	mux := m.GetEventMetadataMutex()
	if mux == nil {
		t.Error("GetEventMetadataMutex() should not be nil")
	}
}

func TestAgentOutputManagerEnsureAsyncOutputWorkerRunsOnce(t *testing.T) {
	t.Parallel()

	m := NewAgentOutputManager()

	count := 0
	fn := func() {
		count++
	}

	m.EnsureAsyncOutputWorker(fn)
	m.EnsureAsyncOutputWorker(fn)
	m.EnsureAsyncOutputWorker(fn)

	if count != 1 {
		t.Errorf("EnsureAsyncOutputWorker ran %d times; want 1", count)
	}
}

func TestAgentOutputManagerEventMetadataConcurrent(t *testing.T) {
	t.Parallel()

	m := NewAgentOutputManager()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			meta := make(map[string]interface{})
			meta["counter"] = n
			m.SetEventMetadata(meta)
		}(i)
		go func() {
			defer wg.Done()
			// Just read — verify no race
			m.GetEventMetadata()
		}()
	}
	wg.Wait()
}

// === AgentSecurityManager ===

func TestNewAgentSecurityManagerDefaults(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	if m.GetSecurityApprovalMgr() == nil {
		t.Error("GetSecurityApprovalMgr() is nil; want non-nil")
	}
	if m.GetOutputRedactor() == nil {
		t.Error("GetOutputRedactor() is nil; want non-nil")
	}
	if m.GetElevationGate() == nil {
		t.Error("GetElevationGate() is nil; want non-nil")
	}
	if m.GetUnsafeMode() {
		t.Error("GetUnsafeMode() is true; want false")
	}
	if m.IsSecurityBypassApproved() {
		t.Error("IsSecurityBypassApproved() is true; want false")
	}
	if m.HasActiveWebUIClients() {
		t.Error("HasActiveWebUIClients() is true; want false (no function set)")
	}
}

func TestAgentSecurityManagerUnsafeMode(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	m.SetUnsafeMode(true)
	if !m.GetUnsafeMode() {
		t.Error("SetUnsafeMode(true) then GetUnsafeMode() should be true")
	}
	m.SetUnsafeMode(false)
	if m.GetUnsafeMode() {
		t.Error("SetUnsafeMode(false) then GetUnsafeMode() should be false")
	}
}

func TestAgentSecurityManagerBypassApproved(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	if m.IsSecurityBypassApproved() {
		t.Error("should not be approved initially")
	}

	m.SetSecurityBypassApproved()
	if !m.IsSecurityBypassApproved() {
		t.Error("should be approved after SetSecurityBypassApproved()")
	}

	// Calling again should still be true (idempotent)
	m.SetSecurityBypassApproved()
	if !m.IsSecurityBypassApproved() {
		t.Error("should still be approved after second call")
	}
}

func TestAgentSecurityManagerConcernIgnored(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	// Initially not ignored
	if m.IsConcernIgnored("file.go", "concern-A") {
		t.Error("should not be ignored initially")
	}

	// Set and check
	m.SetConcernIgnored("file.go", "concern-A")
	if !m.IsConcernIgnored("file.go", "concern-A") {
		t.Error("should be ignored after SetConcernIgnored")
	}

	// Different file — not ignored
	if m.IsConcernIgnored("other.go", "concern-A") {
		t.Error("different file should not be ignored")
	}

	// Same file, different concern — not ignored
	if m.IsConcernIgnored("file.go", "concern-B") {
		t.Error("different concern on same file should not be ignored")
	}

	// Set second concern on same file
	m.SetConcernIgnored("file.go", "concern-B")
	if !m.IsConcernIgnored("file.go", "concern-B") {
		t.Error("should be ignored after SetConcernIgnored for concern-B")
	}
	// First concern should still be ignored
	if !m.IsConcernIgnored("file.go", "concern-A") {
		t.Error("concern-A should still be ignored")
	}
}

func TestAgentSecurityManagerHasActiveWebUIClients(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	// No function set
	if m.HasActiveWebUIClients() {
		t.Error("should be false when no function set")
	}

	// Set function returning true
	m.SetHasActiveWebUIClients(func() bool { return true })
	if !m.HasActiveWebUIClients() {
		t.Error("should be true when function returns true")
	}

	// Set function returning false
	m.SetHasActiveWebUIClients(func() bool { return false })
	if m.HasActiveWebUIClients() {
		t.Error("should be false when function returns false")
	}
}

func TestAgentSecurityManagerSetElevationGate(t *testing.T) {
	t.Parallel()

	m := NewAgentSecurityManager()

	oldGate := m.GetElevationGate()
	if oldGate == nil {
		t.Fatal("default elevation gate should be non-nil")
	}

	// Replace with nil
	m.SetElevationGate(nil)
	if m.GetElevationGate() != nil {
		t.Error("SetElevationGate(nil) then GetElevationGate() should be nil")
	}
}

// === AgentStateManager ===

func TestNewAgentStateManagerDefaults(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	// Messages
	msgs := s.GetMessages()
	if msgs == nil || len(msgs) != 0 {
		t.Error("messages should be empty slice")
	}

	// History
	if s.GetHistoryIndex() != -1 {
		t.Errorf("historyIndex should be -1; got %d", s.GetHistoryIndex())
	}
	hist := s.GetCommandHistory()
	if hist == nil || len(hist) != 0 {
		t.Error("commandHistory should be empty slice")
	}

	// Active persona
	if s.GetActivePersona() != "orchestrator" {
		t.Errorf("activePersona should be 'orchestrator'; got %q", s.GetActivePersona())
	}

	// Optimizer/pruner/circuitBreaker should be non-nil
	if s.GetOptimizer() == nil {
		t.Error("optimizer should be non-nil")
	}
	if s.GetConversationPruner() == nil {
		t.Error("conversationPruner should be non-nil")
	}
	if s.GetCircuitBreaker() == nil {
		t.Error("circuitBreaker should be non-nil")
	}

	// False stop detection
	if !s.IsFalseStopDetectionEnabled() {
		t.Error("falseStopDetectionEnabled should be true by default")
	}

	// Counters should be zero
	if s.GetTotalCost() != 0 {
		t.Error("totalCost should be 0")
	}
	if s.GetTotalTokens() != 0 {
		t.Error("totalTokens should be 0")
	}
	if s.GetPromptTokens() != 0 {
		t.Error("promptTokens should be 0")
	}
	if s.GetCompletionTokens() != 0 {
		t.Error("completionTokens should be 0")
	}
	if s.GetLLMCallCount() != 0 {
		t.Error("llmCallCount should be 0")
	}
	if s.GetCurrentIteration() != 0 {
		t.Errorf("currentIteration should be 0; got %d", s.GetCurrentIteration())
	}

	// Mutexes non-nil
	if s.GetCheckpointMutex() == nil {
		t.Error("checkpointMu should be non-nil")
	}
	if s.GetTaskActionsMutex() == nil {
		t.Error("taskActionsMu should be non-nil")
	}
	if s.GetHistoryMutex() == nil {
		t.Error("historyMu should be non-nil")
	}
	if s.GetPauseMutex() == nil {
		t.Error("pauseMutex should be non-nil")
	}
}

func TestAgentStateManagerMessages(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	msg := api.Message{Role: "user", Content: "hello"}
	s.AddMessage(msg)

	msgs := s.GetMessages()
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Error("AddMessage should append message")
	}

	// SetMessages replaces all
	newMsgs := []api.Message{{Role: "system", Content: "new system"}}
	s.SetMessages(newMsgs)
	msgs = s.GetMessages()
	if len(msgs) != 1 || msgs[0].Role != "system" {
		t.Error("SetMessages should replace all messages")
	}
}

func TestAgentStateManagerSession(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetSessionID("test-session")
	if s.GetSessionID() != "test-session" {
		t.Errorf("sessionID = %q; want 'test-session'", s.GetSessionID())
	}
}

func TestAgentStateManagerTurnCheckpoints(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	cp := TurnCheckpoint{StartIndex: 0, EndIndex: 5, Summary: "test summary"}
	s.AddTurnCheckpoint(cp)
	cps := s.GetTurnCheckpoints()
	if len(cps) != 1 {
		t.Error("AddTurnCheckpoint should add one checkpoint")
	}

	s.SetTurnCheckpoints([]TurnCheckpoint{})
	if len(s.GetTurnCheckpoints()) != 0 {
		t.Error("SetTurnCheckpoints should replace all")
	}
}

func TestAgentStateManagerSummary(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetPreviousSummary("my summary")
	if s.GetPreviousSummary() != "my summary" {
		t.Errorf("GetPreviousSummary = %q; want 'my summary'", s.GetPreviousSummary())
	}
}

func TestAgentStateManagerOptimizer(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	orig := s.GetOptimizer()
	newOpt := NewConversationOptimizer(false, false)
	s.SetOptimizer(newOpt)
	if s.GetOptimizer() != newOpt {
		t.Error("SetOptimizer should replace optimizer")
	}
	s.SetOptimizer(orig) // restore
}

func TestAgentStateManagerContextTokens(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetCurrentContextTokens(5000)
	if s.GetCurrentContextTokens() != 5000 {
		t.Errorf("currentContextTokens = %d; want 5000", s.GetCurrentContextTokens())
	}

	s.SetMaxContextTokens(100000)
	if s.GetMaxContextTokens() != 100000 {
		t.Errorf("maxContextTokens = %d; want 100000", s.GetMaxContextTokens())
	}
}

func TestAgentStateManagerContextWarning(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.IsContextWarningIssued() {
		t.Error("contextWarningIssued should be false by default")
	}

	s.SetContextWarningIssued(true)
	if !s.IsContextWarningIssued() {
		t.Error("should be true after SetContextWarningIssued(true)")
	}
}

func TestAgentStateManagerTaskActions(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	action := TaskAction{Type: "file_read", Description: "read foo.go"}
	s.AddTaskAction(action)
	actions := s.GetTaskActions()
	if len(actions) != 1 || actions[0].Type != "file_read" {
		t.Error("AddTaskAction should append action")
	}

	newActions := []TaskAction{{Type: "file_created"}}
	s.SetTaskActions(newActions)
	if len(s.GetTaskActions()) != 1 || s.GetTaskActions()[0].Type != "file_created" {
		t.Error("SetTaskActions should replace all")
	}
}

func TestAgentStateManagerCost(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.AddCost(1.5)
	if s.GetTotalCost() != 1.5 {
		t.Errorf("totalCost = %f; want 1.5", s.GetTotalCost())
	}

	s.AddCost(2.5)
	if s.GetTotalCost() != 4.0 {
		t.Errorf("totalCost = %f; want 4.0 (accumulated)", s.GetTotalCost())
	}

	s.SetTotalCost(10.0)
	if s.GetTotalCost() != 10.0 {
		t.Errorf("totalCost = %f; want 10.0", s.GetTotalCost())
	}
}

func TestAgentStateManagerTokenCounts(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetTotalTokens(1000)
	if s.GetTotalTokens() != 1000 {
		t.Errorf("totalTokens = %d; want 1000", s.GetTotalTokens())
	}

	s.SetPromptTokens(800)
	if s.GetPromptTokens() != 800 {
		t.Errorf("promptTokens = %d; want 800", s.GetPromptTokens())
	}

	s.SetCompletionTokens(200)
	if s.GetCompletionTokens() != 200 {
		t.Errorf("completionTokens = %d; want 200", s.GetCompletionTokens())
	}

	s.SetEstimatedTokenResponses(150)
	if s.GetEstimatedTokenResponses() != 150 {
		t.Errorf("estimatedTokenResponses = %d; want 150", s.GetEstimatedTokenResponses())
	}

	s.SetCachedTokens(50)
	if s.GetCachedTokens() != 50 {
		t.Errorf("cachedTokens = %d; want 50", s.GetCachedTokens())
	}

	s.SetCachedCostSavings(0.5)
	if s.GetCachedCostSavings() != 0.5 {
		t.Errorf("cachedCostSavings = %f; want 0.5", s.GetCachedCostSavings())
	}
}

func TestAgentStateManagerLLMCallCount(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.GetLLMCallCount() != 0 {
		t.Error("should start at 0")
	}

	s.IncrementLLMCallCount()
	if s.GetLLMCallCount() != 1 {
		t.Errorf("should be 1 after one increment; got %d", s.GetLLMCallCount())
	}

	s.IncrementLLMCallCount()
	s.IncrementLLMCallCount()
	if s.GetLLMCallCount() != 3 {
		t.Errorf("should be 3 after three increments; got %d", s.GetLLMCallCount())
	}

	s.SetLLMCallCount(10)
	if s.GetLLMCallCount() != 10 {
		t.Errorf("should be 10 after SetLLMCallCount(10); got %d", s.GetLLMCallCount())
	}
}

func TestAgentStateManagerSkillsAndPersona(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetActiveSkills([]string{"skill-a", "skill-b"})
	skills := s.GetActiveSkills()
	if len(skills) != 2 || skills[0] != "skill-a" {
		t.Error("SetActiveSkills should replace skills")
	}

	s.SetActivePersona("coder")
	if s.GetActivePersona() != "coder" {
		t.Errorf("GetActivePersona = %q; want 'coder'", s.GetActivePersona())
	}
}

func TestAgentStateManagerCircuitBreaker(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	orig := s.GetCircuitBreaker()
	newCB := &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)}
	s.SetCircuitBreaker(newCB)
	if s.GetCircuitBreaker() != newCB {
		t.Error("SetCircuitBreaker should replace")
	}
	s.SetCircuitBreaker(orig) // restore
}

func TestAgentStateManagerToolCallGuidance(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.IsToolCallGuidanceAdded() {
		t.Error("should be false by default")
	}
	s.SetToolCallGuidanceAdded(true)
	if !s.IsToolCallGuidanceAdded() {
		t.Error("should be true after SetToolCallGuidanceAdded(true)")
	}
}

func TestAgentStateManagerPendingState(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetPendingSwitchContextRefresh("refresh-value")
	if s.GetPendingSwitchContextRefresh() != "refresh-value" {
		t.Errorf("pendingSwitchContextRefresh = %q; want 'refresh-value'", s.GetPendingSwitchContextRefresh())
	}

	s.SetPendingStrictSwitchNotice("notice-value")
	if s.GetPendingStrictSwitchNotice() != "notice-value" {
		t.Errorf("pendingStrictSwitchNotice = %q; want 'notice-value'", s.GetPendingStrictSwitchNotice())
	}

	s.SetPendingSystemSupplement("supplement-value")
	if s.GetPendingSystemSupplement() != "supplement-value" {
		t.Errorf("pendingSystemSupplement = %q; want 'supplement-value'", s.GetPendingSystemSupplement())
	}
}

func TestAgentStateManagerFalseStopDetection(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if !s.IsFalseStopDetectionEnabled() {
		t.Error("should be true by default")
	}
	s.SetFalseStopDetectionEnabled(false)
	if s.IsFalseStopDetectionEnabled() {
		t.Error("should be false after SetFalseStopDetectionEnabled(false)")
	}
}

func TestAgentStateManagerTermination(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetLastRunTerminationReason("max_iterations")
	if s.GetLastRunTerminationReason() != "max_iterations" {
		t.Errorf("lastRunTerminationReason = %q; want 'max_iterations'", s.GetLastRunTerminationReason())
	}
}

func TestAgentStateManagerConversationPruner(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	orig := s.GetConversationPruner()
	newPruner := NewConversationPruner(true)
	s.SetConversationPruner(newPruner)
	if s.GetConversationPruner() != newPruner {
		t.Error("SetConversationPruner should replace")
	}
	s.SetConversationPruner(orig) // restore
}

func TestAgentStateManagerCommandHistory(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetCommandHistory([]string{"cmd1", "cmd2"})
	hist := s.GetCommandHistory()
	if len(hist) != 2 || hist[0] != "cmd1" {
		t.Error("SetCommandHistory should replace history")
	}

	s.SetHistoryIndex(5)
	if s.GetHistoryIndex() != 5 {
		t.Errorf("historyIndex = %d; want 5", s.GetHistoryIndex())
	}
}

func TestAgentStateManagerPauseState(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.GetPauseState() != nil {
		t.Error("pauseState should be nil by default")
	}

	ps := &PauseState{IsPaused: true}
	s.SetPauseState(ps)
	if s.GetPauseState() != ps {
		t.Error("SetPauseState should replace")
	}
}

func TestAgentStateManagerTraceSession(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.GetTraceSession() != nil {
		t.Error("traceSession should be nil by default")
	}

	s.SetTraceSession("trace-value")
	if s.GetTraceSession() != "trace-value" {
		t.Error("SetTraceSession should store value")
	}
}

func TestAgentStateManagerSessionConfig(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	s.SetSessionModel("claude-sonnet-4")
	if s.GetSessionModel() != "claude-sonnet-4" {
		t.Errorf("sessionModel = %q; want 'claude-sonnet-4'", s.GetSessionModel())
	}
}

func TestAgentStateManagerConfigOverrides(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	overrides := map[string]interface{}{"max_tokens": 4096}
	s.SetConfigOverrides(overrides)
	got := s.GetConfigOverrides()
	if got == nil || got["max_tokens"] != 4096 {
		t.Error("SetConfigOverrides should store map")
	}
}

func TestAgentStateManagerCurrentIteration(t *testing.T) {
	t.Parallel()

	s := NewAgentStateManager(false)

	if s.GetCurrentIteration() != 0 {
		t.Errorf("currentIteration should be 0; got %d", s.GetCurrentIteration())
	}

	s.SetCurrentIteration(5)
	if s.GetCurrentIteration() != 5 {
		t.Errorf("currentIteration = %d; want 5", s.GetCurrentIteration())
	}
}
