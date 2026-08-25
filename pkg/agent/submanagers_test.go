package agent

import (
	"errors"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/security"
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

	m.AddSessionAllowedFolder("/tmp/audited-folder")
	if !m.IsSecurityBypassApproved() {
		t.Error("should be approved after AddSessionAllowedFolder")
	}

	// Adding the same folder is a no-op (dedup).
	m.AddSessionAllowedFolder("/tmp/audited-folder")
	if !m.IsSecurityBypassApproved() {
		t.Error("should still be approved after duplicate add")
	}
	if got := len(m.SnapshotSessionAllowedFolders()); got != 1 {
		t.Errorf("expected 1 folder after duplicate add, got %d", got)
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

func TestNewAgentMCPManager(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetManager() == nil {
		t.Error("should have default MCP manager")
	}
	if mm.GetToolsCache() != nil {
		t.Errorf("tools cache should be nil by default, got %v", mm.GetToolsCache())
	}
	if mm.IsInitialized() {
		t.Error("should not be initialized by default")
	}
	if mm.GetInitError() != nil {
		t.Errorf("init error should be nil by default, got %v", mm.GetInitError())
	}
}

func TestAgentMCPManager_Manager(t *testing.T) {
	mm := NewAgentMCPManager()

	mgr := mm.GetManager()
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	// Replace manager
	newMgr := mm.GetManager()
	mm.SetManager(newMgr)
	if mm.GetManager() != newMgr {
		t.Error("should return replaced manager")
	}

	// Set to nil
	mm.SetManager(nil)
	if mm.GetManager() != nil {
		t.Error("should return nil after setting nil")
	}
}

func TestAgentMCPManager_ToolsCache(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetToolsCache() != nil {
		t.Error("tools cache should be nil by default")
	}

	tools := []api.Tool{
		{Type: "function", Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "tool1", Description: "First tool", Parameters: nil}},
		{Type: "function", Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "tool2", Description: "Second tool", Parameters: nil}},
	}
	mm.SetToolsCache(tools)
	got := mm.GetToolsCache()
	if len(got) != 2 {
		t.Errorf("GetToolsCache = %d, want 2", len(got))
	}
	if got[0].Function.Name != "tool1" {
		t.Errorf("first tool name = %q, want tool1", got[0].Function.Name)
	}

	// Replace with empty
	mm.SetToolsCache([]api.Tool{})
	if len(mm.GetToolsCache()) != 0 {
		t.Error("should be empty after setting empty slice")
	}

	// Set to nil
	mm.SetToolsCache(nil)
	if mm.GetToolsCache() != nil {
		t.Error("should be nil after setting nil")
	}
}

func TestAgentMCPManager_Initialized(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.IsInitialized() {
		t.Error("should not be initialized by default")
	}

	mm.SetInitialized(true)
	if !mm.IsInitialized() {
		t.Error("should be true after setting")
	}

	mm.SetInitialized(false)
	if mm.IsInitialized() {
		t.Error("should be false after resetting")
	}
}

func TestAgentMCPManager_InitError(t *testing.T) {
	mm := NewAgentMCPManager()

	if mm.GetInitError() != nil {
		t.Error("init error should be nil by default")
	}

	err := errors.New("connection refused")
	mm.SetInitError(err)
	if mm.GetInitError() != err {
		t.Error("should return set error")
	}

	// Clear error
	mm.SetInitError(nil)
	if mm.GetInitError() != nil {
		t.Error("should be nil after clearing")
	}
}

func TestAgentMCPManager_LockUnlockInit(t *testing.T) {
	mm := NewAgentMCPManager()

	// Lock and unlock should not panic
	mm.LockInit()
	mm.UnlockInit()
}

func TestAgentMCPManager_ConcurrentAccess(t *testing.T) {
	mm := NewAgentMCPManager()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mm.SetInitialized(n%2 == 0)
			mm.SetInitError(errors.New("error " + string(rune('a'+n%26))))
			mm.SetToolsCache([]api.Tool{{Type: "function", Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{Name: "tool-" + string(rune('a'+n%26)), Description: "", Parameters: nil}}})
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			mm.IsInitialized()
			mm.GetInitError()
			mm.GetToolsCache()
			mm.GetManager()
		}()
	}
	wg.Wait()

	// Should not have panicked
	_ = mm.IsInitialized()
}

func TestAgentMCPManager_ConcurrentLockUnlock(t *testing.T) {
	mm := NewAgentMCPManager()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mm.LockInit()
			mm.UnlockInit()
		}()
	}
	wg.Wait()
}

func TestAgentMCPManager_AllFieldsDefault(t *testing.T) {
	mm := NewAgentMCPManager()

	// Verify all defaults are correct
	if mm.GetManager() == nil {
		t.Error("manager should not be nil")
	}
	if mm.GetToolsCache() != nil {
		t.Error("tools cache should be nil")
	}
	if mm.IsInitialized() {
		t.Error("should not be initialized")
	}
	if mm.GetInitError() != nil {
		t.Error("init error should be nil")
	}
}

func TestNewAgentOutputManager(t *testing.T) {
	om := NewAgentOutputManager()

	if om.IsStreamingEnabled() {
		t.Error("streaming should be disabled by default")
	}
	if om.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil by default")
	}
	if om.GetReasoningCallback() != nil {
		t.Error("reasoning callback should be nil by default")
	}
	if om.GetFlushCallback() != nil {
		t.Error("flush callback should be nil by default")
	}
	if om.GetOutputMutex() != nil {
		t.Error("output mutex should be nil by default")
	}
	if om.GetOutputRouter() != nil {
		t.Error("output router should be nil by default")
	}
	if om.GetAsyncOutput() != nil {
		t.Error("async output channel should be nil by default")
	}
	if om.GetEventMetadata() == nil {
		t.Error("event metadata should be initialized (non-nil) by default")
	}
	if om.GetEventMetadataMutex() == nil {
		t.Error("event metadata mutex should not be nil")
	}
}

func TestAgentOutputManager_StreamingEnabled(t *testing.T) {
	om := NewAgentOutputManager()

	if om.IsStreamingEnabled() {
		t.Error("should be false initially")
	}

	om.SetStreamingEnabled(true)
	if !om.IsStreamingEnabled() {
		t.Error("should be true after setting")
	}

	om.SetStreamingEnabled(false)
	if om.IsStreamingEnabled() {
		t.Error("should be false after resetting")
	}
}

func TestAgentOutputManager_StreamingCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil by default")
	}

	called := false
	cb := func(s string) { called = true }
	om.SetStreamingCallback(cb)
	if om.GetStreamingCallback() == nil {
		t.Error("GetStreamingCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetStreamingCallback()("test")
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_ReasoningCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetReasoningCallback() != nil {
		t.Error("reasoning callback should be nil by default")
	}

	called := false
	cb := func(s string) { called = true }
	om.SetReasoningCallback(cb)
	if om.GetReasoningCallback() == nil {
		t.Error("GetReasoningCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetReasoningCallback()("test")
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_FlushCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetFlushCallback() != nil {
		t.Error("flush callback should be nil by default")
	}

	called := false
	cb := func() { called = true }
	om.SetFlushCallback(cb)
	if om.GetFlushCallback() == nil {
		t.Error("GetFlushCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetFlushCallback()()
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_OutputMutex(t *testing.T) {
	om := NewAgentOutputManager()

	mu := &sync.Mutex{}
	om.SetOutputMutex(mu)
	if om.GetOutputMutex() != mu {
		t.Error("GetOutputMutex should return set mutex")
	}
}

func TestAgentOutputManager_StreamingBuffer(t *testing.T) {
	om := NewAgentOutputManager()

	buf := om.GetStreamingBuffer()
	if buf == nil {
		t.Error("GetStreamingBuffer should not be nil")
	}

	buf.WriteString("hello")
	if buf.String() != "hello" {
		t.Errorf("buffer content = %q, want hello", buf.String())
	}

	// Verify it's the same buffer (reference equality)
	buf2 := om.GetStreamingBuffer()
	if buf2.String() != "hello" {
		t.Error("should return the same buffer instance")
	}
}

func TestAgentOutputManager_ReasoningBuffer(t *testing.T) {
	om := NewAgentOutputManager()

	buf := om.GetReasoningBuffer()
	if buf == nil {
		t.Error("GetReasoningBuffer should not be nil")
	}

	buf.WriteString("reasoning")
	if buf.String() != "reasoning" {
		t.Errorf("buffer content = %q, want reasoning", buf.String())
	}
}

func TestAgentOutputManager_OutputRouter(t *testing.T) {
	om := NewAgentOutputManager()

	router := &OutputRouter{}
	om.SetOutputRouter(router)
	if om.GetOutputRouter() != router {
		t.Error("GetOutputRouter should return set router")
	}
}

func TestAgentOutputManager_AsyncOutput(t *testing.T) {
	om := NewAgentOutputManager()

	ch := make(chan string, 1)
	om.SetAsyncOutput(ch)
	if om.GetAsyncOutput() != ch {
		t.Error("GetAsyncOutput should return set channel")
	}
}

func TestAgentOutputManager_EnsureAsyncOutputWorker(t *testing.T) {
	om := NewAgentOutputManager()

	callCount := 0
	fn := func() {
		callCount++
	}

	// First call should execute the function
	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("first call should execute once, got %d", callCount)
	}

	// Subsequent calls should NOT execute (sync.Once behavior)
	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("second call should NOT execute (sync.Once), got %d", callCount)
	}

	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("third call should NOT execute (sync.Once), got %d", callCount)
	}
}

func TestAgentOutputManager_EnsureAsyncOutputWorker_DifferentFunctions(t *testing.T) {
	om := NewAgentOutputManager()

	count1 := 0
	count2 := 0

	fn1 := func() { count1++ }
	fn2 := func() { count2++ }

	// First function fires
	om.EnsureAsyncOutputWorker(fn1)
	// Second function should NOT fire (Once is already done)
	om.EnsureAsyncOutputWorker(fn2)

	if count1 != 1 {
		t.Errorf("fn1 called %d times, want 1", count1)
	}
	if count2 != 0 {
		t.Errorf("fn2 called %d times, want 0 (Once already triggered)", count2)
	}
}

func TestAgentOutputManager_AsyncBufferSize(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetAsyncBufferSize() != 0 {
		t.Errorf("default async buffer size = %d, want 0", om.GetAsyncBufferSize())
	}

	om.SetAsyncBufferSize(10)
	if om.GetAsyncBufferSize() != 10 {
		t.Errorf("GetAsyncBufferSize = %d, want 10", om.GetAsyncBufferSize())
	}
}

func TestAgentOutputManager_EventMetadata(t *testing.T) {
	om := NewAgentOutputManager()

	// Default is empty map
	meta := om.GetEventMetadata()
	if meta == nil {
		t.Error("default metadata should be non-nil")
	}
	if len(meta) != 0 {
		t.Errorf("default metadata should be empty, got %d entries", len(meta))
	}

	// Set new metadata
	om.SetEventMetadata(map[string]interface{}{"key": "value"})
	meta = om.GetEventMetadata()
	if meta["key"] != "value" {
		t.Errorf("metadata[key] = %v, want value", meta["key"])
	}
}

func TestAgentOutputManager_SetEventMetadataUnlocked(t *testing.T) {
	om := NewAgentOutputManager()

	// SetEventMetadataUnlocked sets without mutex
	om.SetEventMetadataUnlocked(map[string]interface{}{"unlocked": true})
	meta := om.GetEventMetadata()
	if meta["unlocked"] != true {
		t.Error("SetEventMetadataUnlocked should set the value")
	}
}

func TestAgentOutputManager_EventMetadataMutex(t *testing.T) {
	om := NewAgentOutputManager()

	mu := om.GetEventMetadataMutex()
	if mu == nil {
		t.Error("GetEventMetadataMutex should not be nil")
	}

	// Verify we can lock and unlock
	mu.Lock()
	mu.Unlock()
}

func TestAgentOutputManager_ConcurrentMetadataAccess(t *testing.T) {
	om := NewAgentOutputManager()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			om.SetEventMetadata(map[string]interface{}{"key": n})
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			om.GetEventMetadata()
		}()
	}
	wg.Wait()

	// Should not have panicked
	meta := om.GetEventMetadata()
	if meta == nil {
		t.Error("metadata should still be non-nil after concurrent access")
	}
}

func TestAgentOutputManager_StreamingBufferIsolation(t *testing.T) {
	om := NewAgentOutputManager()

	streamingBuf := om.GetStreamingBuffer()
	reasoningBuf := om.GetReasoningBuffer()

	streamingBuf.WriteString("stream")
	reasoningBuf.WriteString("reason")

	if streamingBuf.String() != "stream" {
		t.Errorf("streaming buffer = %q, want stream", streamingBuf.String())
	}
	if reasoningBuf.String() != "reason" {
		t.Errorf("reasoning buffer = %q, want reason", reasoningBuf.String())
	}

	// Buffers should be different
	if streamingBuf.String() == reasoningBuf.String() {
		t.Error("streaming and reasoning buffers should be separate")
	}
}

func TestAgentOutputManager_BuffersPreallocate(t *testing.T) {
	om := NewAgentOutputManager()

	sb := om.GetStreamingBuffer()
	rb := om.GetReasoningBuffer()

	// Writers should start empty
	if sb.Len() != 0 {
		t.Errorf("streaming buffer length = %d, want 0", sb.Len())
	}
	if rb.Len() != 0 {
		t.Errorf("reasoning buffer length = %d, want 0", rb.Len())
	}

	// Multiple getters should return references to the same buffers
	sb.WriteString("data")
	if om.GetStreamingBuffer().String() != "data" {
		t.Error("should return same buffer reference")
	}
}

func TestNewAgentSecurityManager(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.GetSecurityApprovalMgr() == nil {
		t.Error("should have default security approval manager")
	}
	if sm.GetUnsafeMode() {
		t.Error("unsafe mode should be false by default")
	}
	if sm.IsSecurityBypassApproved() {
		t.Error("security bypass should not be approved by default")
	}
	if sm.GetOutputRedactor() == nil {
		t.Error("should have default output redactor")
	}
	if sm.GetElevationGate() == nil {
		t.Error("should have default elevation gate")
	}
	if sm.HasActiveWebUIClients() {
		t.Error("should return false when no callback is set")
	}
}

func TestAgentSecurityManager_SecurityApprovalMgr(t *testing.T) {
	sm := NewAgentSecurityManager()

	mgr := sm.GetSecurityApprovalMgr()
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}

	// Replace
	newMgr := security.NewApprovalManager()
	sm.securityApprovalMgr = newMgr
	if sm.GetSecurityApprovalMgr() != newMgr {
		t.Error("should return replaced manager")
	}
}

func TestAgentSecurityManager_UnsafeMode(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetUnsafeMode(true)
	if !sm.GetUnsafeMode() {
		t.Error("should return true after setting")
	}

	sm.SetUnsafeMode(false)
	if sm.GetUnsafeMode() {
		t.Error("should return false after resetting")
	}
}

func TestAgentSecurityManager_SecurityBypass(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsSecurityBypassApproved() {
		t.Error("should not be approved by default")
	}

	// Adding any folder to the session allowlist flips
	// IsSecurityBypassApproved to true (coarse "user consented to
	// some external access" signal).
	sm.AddSessionAllowedFolder("/tmp/foo")
	if !sm.IsSecurityBypassApproved() {
		t.Error("should be approved after adding a folder")
	}

	// Adding the same folder again is a no-op (dedup).
	sm.AddSessionAllowedFolder("/tmp/foo")
	if !sm.IsSecurityBypassApproved() {
		t.Error("should remain approved")
	}
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 1 {
		t.Errorf("expected 1 folder after dup add, got %d", got)
	}
}

func TestAgentSecurityManager_OutputRedactor(t *testing.T) {
	sm := NewAgentSecurityManager()

	redactor := sm.GetOutputRedactor()
	if redactor == nil {
		t.Error("output redactor should not be nil")
	}
}

func TestAgentSecurityManager_ElevationGate(t *testing.T) {
	sm := NewAgentSecurityManager()

	gate := sm.GetElevationGate()
	if gate == nil {
		t.Error("elevation gate should not be nil")
	}

	// Replace
	newGate := security.NewElevationGate(nil)
	sm.SetElevationGate(newGate)
	if sm.GetElevationGate() != newGate {
		t.Error("should return replaced gate")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_NoCallback(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.HasActiveWebUIClients() {
		t.Error("should return false when no callback is set")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_CallbackTrue(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetHasActiveWebUIClients(func() bool { return true })
	if !sm.HasActiveWebUIClients() {
		t.Error("should return true when callback returns true")
	}
}

func TestAgentSecurityManager_HasActiveWebUIClients_CallbackFalse(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetHasActiveWebUIClients(func() bool { return false })
	if sm.HasActiveWebUIClients() {
		t.Error("should return false when callback returns false")
	}
}

func TestAgentSecurityManager_ConcernIgnored_Empty(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsConcernIgnored("file.go", "insecure") {
		t.Error("should return false when no concerns are ignored")
	}
}

func TestAgentSecurityManager_ConcernIgnored_SetAndGet(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "insecure")

	if !sm.IsConcernIgnored("file.go", "insecure") {
		t.Error("should return true after setting")
	}

	// Different file
	if sm.IsConcernIgnored("other.go", "insecure") {
		t.Error("should return false for different file")
	}

	// Different concern on same file
	if sm.IsConcernIgnored("file.go", "other_concern") {
		t.Error("should return false for different concern")
	}
}

func TestAgentSecurityManager_ConcernIgnored_MultipleConcerns(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "concern_a")
	sm.SetConcernIgnored("file.go", "concern_b")
	sm.SetConcernIgnored("file2.go", "concern_a")

	if !sm.IsConcernIgnored("file.go", "concern_a") {
		t.Error("should find concern_a for file.go")
	}
	if !sm.IsConcernIgnored("file.go", "concern_b") {
		t.Error("should find concern_b for file.go")
	}
	if !sm.IsConcernIgnored("file2.go", "concern_a") {
		t.Error("should find concern_a for file2.go")
	}
	if sm.IsConcernIgnored("file2.go", "concern_b") {
		t.Error("should not find concern_b for file2.go")
	}
}

func TestAgentSecurityManager_ConcernIgnored_Idempotent(t *testing.T) {
	sm := NewAgentSecurityManager()

	sm.SetConcernIgnored("file.go", "concern")
	sm.SetConcernIgnored("file.go", "concern") // set again

	// Should still find it
	if !sm.IsConcernIgnored("file.go", "concern") {
		t.Error("should still find concern after idempotent set")
	}
}

func TestAgentSecurityManager_ConcurrentAccess(t *testing.T) {
	sm := NewAgentSecurityManager()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			file := "file" + string(rune('a'+n%26)) + ".go"
			sm.SetConcernIgnored(file, "concern")
			if n%2 == 0 {
				sm.SetUnsafeMode(true)
			} else {
				sm.SetUnsafeMode(false)
			}
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.GetUnsafeMode()
			sm.IsSecurityBypassApproved()
			sm.IsConcernIgnored("file.go", "concern")
		}()
	}
	wg.Wait()

	// Should not have panicked
	_ = sm.GetUnsafeMode()
}

// =============================================================================
// SP-049-3a: Unsafe shell mode tests
// =============================================================================

func TestAgentSecurityManager_UnsafeShellMode_Basic(t *testing.T) {
	sm := NewAgentSecurityManager()

	// Default should be false.
	if sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be false by default")
	}

	// Enable.
	sm.SetUnsafeShellMode(true)
	if !sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be true after setting")
	}

	// Disable again.
	sm.SetUnsafeShellMode(false)
	if sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be false after resetting")
	}
}

func TestAgentSecurityManager_UnsafeShellMode_IndependentOfUnsafeMode(t *testing.T) {
	sm := NewAgentSecurityManager()

	// Setting unsafe mode should NOT affect unsafe shell mode.
	sm.SetUnsafeMode(true)
	if sm.GetUnsafeShellMode() {
		t.Error("SetUnsafeMode should not change GetUnsafeShellMode")
	}
	if !sm.GetUnsafeMode() {
		t.Error("unsafe mode should be true")
	}

	// Setting unsafe shell mode should NOT affect unsafe mode.
	sm.SetUnsafeShellMode(true)
	if sm.GetUnsafeMode() != true {
		t.Error("SetUnsafeShellMode should not change GetUnsafeMode (still true)")
	}
	sm.SetUnsafeMode(false)
	sm.SetUnsafeShellMode(true)
	if sm.GetUnsafeMode() {
		t.Error("unsafe mode should remain false after SetUnsafeShellMode")
	}
	if !sm.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be true")
	}
}

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

// TestAgentStateManager_SessionProvider tests GetSessionProvider and SetSessionProvider
// with mutex protection.
func TestAgentStateManager_SessionProvider(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Test zero values
	if got := sm.GetSessionProvider(); got != "" {
		t.Errorf("expected empty provider for fresh state, got %v", got)
	}

	// Test setting provider
	sm.SetSessionProvider(api.OpenAIClientType)
	if got := sm.GetSessionProvider(); got != api.OpenAIClientType {
		t.Errorf("expected OpenAIClientType, got %v", got)
	}

	// Test setting to another provider
	sm.SetSessionProvider(api.OllamaClientType)
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType, got %v", got)
	}
}

// TestAgentStateManager_SessionModel tests GetSessionModel and SetSessionModel
// with mutex protection.
func TestAgentStateManager_SessionModel(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Test zero values
	if got := sm.GetSessionModel(); got != "" {
		t.Errorf("expected empty model for fresh state, got %q", got)
	}

	// Test setting model
	sm.SetSessionModel("gpt-4o")
	if got := sm.GetSessionModel(); got != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", got)
	}

	// Test setting to another model
	sm.SetSessionModel("llama3.2")
	if got := sm.GetSessionModel(); got != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", got)
	}
}

// TestAgentStateManager_SessionProviderRace tests that GetSessionProvider and
// SetSessionProvider are race-free when called concurrently.
//
// The race detector is what we're verifying here: under `-race`, this
// test catches any data race on the sessionProvider field. We do NOT
// assert that the read after a write on the same goroutine returns the
// value just written — the Go memory model does not guarantee that
// when multiple writers exist (a concurrent writer can land between
// our Set and our Get on this goroutine). What we DO assert is:
//  1. No race detector reports.
//  2. The value read back is always one of the providers we wrote
//     (the providers slice is the only source of truth).
func TestAgentStateManager_SessionProviderRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	providers := []api.ClientType{api.OpenAIClientType, api.OllamaClientType, api.OpenRouterClientType, api.DeepInfraClientType, api.LMStudioClientType}
	valid := map[api.ClientType]bool{}
	for _, p := range providers {
		valid[p] = true
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			provider := providers[id%len(providers)]
			sm.SetSessionProvider(provider)
			got := sm.GetSessionProvider()
			if !valid[got] {
				t.Errorf("goroutine %d: got %v which is not in the providers set", id, got)
			}
		}(i)
	}

	wg.Wait()
}

// TestAgentStateManager_SessionModelRace tests that GetSessionModel and
// SetSessionModel are race-free when called concurrently.
//
// Same reasoning as TestAgentStateManager_SessionProviderRace: under
// `-race` we verify no data race; we do NOT assert the value just
// written is read back (concurrent writers can land between Set and
// Get on the same goroutine). We DO assert the value is always one of
// the models in our write set.
func TestAgentStateManager_SessionModelRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	models := []string{"model-a", "model-b", "model-c", "model-d", "model-e"}
	valid := map[string]bool{}
	for _, m := range models {
		valid[m] = true
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			model := models[id%len(models)]
			sm.SetSessionModel(model)
			got := sm.GetSessionModel()
			if !valid[got] {
				t.Errorf("goroutine %d: got %q which is not in the models set", id, got)
			}
		}(i)
	}

	wg.Wait()
}

// TestAgentStateManager_SessionProviderModel tests that both sessionProvider
// and sessionModel can be set independently and retrieved correctly.
func TestAgentStateManager_SessionProviderModel(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Set both provider and model
	sm.SetSessionProvider(api.OpenRouterClientType)
	sm.SetSessionModel("anthropic/claude-3")

	// Verify both are set correctly
	if got := sm.GetSessionProvider(); got != api.OpenRouterClientType {
		t.Errorf("expected OpenRouterClientType, got %v", got)
	}
	if got := sm.GetSessionModel(); got != "anthropic/claude-3" {
		t.Errorf("expected anthropic/claude-3, got %q", got)
	}

	// Change provider only
	sm.SetSessionProvider(api.OllamaClientType)
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType, got %v", got)
	}
	if got := sm.GetSessionModel(); got != "anthropic/claude-3" {
		t.Errorf("expected anthropic/claude-3 (unchanged), got %q", got)
	}

	// Change model only
	sm.SetSessionModel("llama3.2")
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType (unchanged), got %v", got)
	}
	if got := sm.GetSessionModel(); got != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", got)
	}
}
