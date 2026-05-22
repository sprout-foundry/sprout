package agent

import (
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// StateManager defines the interface for managing conversation state
// previously held directly in the Agent struct.
type StateManager interface {
	// Messages
	GetMessages() []api.Message
	SetMessages([]api.Message)
	AddMessage(api.Message)

	// Session
	GetSessionID() string
	SetSessionID(string)

	// Turn checkpoints
	GetTurnCheckpoints() []TurnCheckpoint
	SetTurnCheckpoints([]TurnCheckpoint)
	AddTurnCheckpoint(TurnCheckpoint)

	// Checkpoint mutex
	GetCheckpointMutex() *sync.RWMutex

	// Summary
	GetPreviousSummary() string
	SetPreviousSummary(string)

	// Optimizer
	GetOptimizer() *ConversationOptimizer
	SetOptimizer(*ConversationOptimizer)

	// Context tokens
	GetCurrentContextTokens() int
	SetCurrentContextTokens(int)
	GetMaxContextTokens() int
	SetMaxContextTokens(int)

	// Context warning
	IsContextWarningIssued() bool
	SetContextWarningIssued(bool)

	// Task actions
	GetTaskActions() []TaskAction
	SetTaskActions([]TaskAction)
	AddTaskAction(TaskAction)

	// Task actions mutex
	GetTaskActionsMutex() *sync.RWMutex

	// Cost
	GetTotalCost() float64
	SetTotalCost(float64)
	AddCost(float64)

	// Token counts
	GetTotalTokens() int
	SetTotalTokens(int)
	GetPromptTokens() int
	SetPromptTokens(int)
	GetCompletionTokens() int
	SetCompletionTokens(int)

	// LLM call tracking
	GetLLMCallCount() int
	SetLLMCallCount(int)
	IncrementLLMCallCount()

	// Tool call tracking
	GetTotalToolCalls() int
	SetTotalToolCalls(int)
	IncrementTotalToolCalls()

	// Estimated token responses
	GetEstimatedTokenResponses() int
	SetEstimatedTokenResponses(int)

	// Cache stats
	GetCachedTokens() int
	SetCachedTokens(int)
	GetCachedCostSavings() float64
	SetCachedCostSavings(float64)

	// Skills and persona
	GetActiveSkills() []string
	SetActiveSkills([]string)
	GetActivePersona() string
	SetActivePersona(string)

	// Circuit breaker
	GetCircuitBreaker() *CircuitBreakerState
	SetCircuitBreaker(*CircuitBreakerState)

	// Tool call guidance
	IsToolCallGuidanceAdded() bool
	SetToolCallGuidanceAdded(bool)

	// Pending state
	GetPendingSwitchContextRefresh() string
	SetPendingSwitchContextRefresh(string)
	GetPendingStrictSwitchNotice() string
	SetPendingStrictSwitchNotice(string)
	GetPendingSystemSupplement() string
	SetPendingSystemSupplement(string)

	// False stop detection
	IsFalseStopDetectionEnabled() bool
	SetFalseStopDetectionEnabled(bool)

	// Termination
	GetLastRunTerminationReason() string
	SetLastRunTerminationReason(string)

	// Conversation pruner
	GetConversationPruner() *ConversationPruner
	SetConversationPruner(*ConversationPruner)

	// Command history
	GetCommandHistory() []string
	SetCommandHistory([]string)
	GetHistoryIndex() int
	SetHistoryIndex(int)
	GetHistoryMutex() *sync.Mutex

	// Pause
	GetPauseState() *PauseState
	SetPauseState(*PauseState)
	GetPauseMutex() *sync.Mutex

	// Tracing
	GetTraceSession() interface{}
	SetTraceSession(interface{})

	// Session config
	GetSessionProvider() api.ClientType
	SetSessionProvider(api.ClientType)
	GetSessionModel() string
	SetSessionModel(string)

	// Config overrides
	GetConfigOverrides() map[string]interface{}
	SetConfigOverrides(map[string]interface{})

	// Current iteration
	GetCurrentIteration() int
	SetCurrentIteration(int)

	// Session intent embedding
	GetSessionIntentEmbedding() []float32
	SetSessionIntentEmbedding([]float32)
	// SetSessionIntentEmbeddingIfNil atomically sets the embedding only if it is
	// currently nil. Returns true if the embedding was set, false if it already
	// had a value. Used to capture the first-turn intent without TOCTOU races.
	SetSessionIntentEmbeddingIfNil(emb []float32) bool
}

// AgentStateManager implements StateManager with simple field-backed getters/setters.
type AgentStateManager struct {
	// General mutex for protecting most shared state
	mu sync.RWMutex

	messages                    []api.Message
	sessionID                   string
	turnCheckpoints             []TurnCheckpoint
	checkpointMu                sync.RWMutex
	previousSummary             string
	optimizer                   *ConversationOptimizer
	currentContextTokens        int
	maxContextTokens            int
	contextWarningIssued        bool
	taskActions                 []TaskAction
	taskActionsMu               sync.RWMutex
	totalCost                   float64
	totalTokens                 int
	promptTokens                int
	completionTokens            int
	llmCallCount                int
	totalToolCalls              int
	estimatedTokenResponses     int
	cachedTokens                int
	cachedCostSavings           float64
	activeSkills                []string
	activePersona               string
	circuitBreaker              *CircuitBreakerState
	toolCallGuidanceAdded       bool
	pendingSwitchContextRefresh string
	pendingStrictSwitchNotice   string
	pendingSystemSupplement     string
	falseStopDetectionEnabled   bool
	lastRunTerminationReason    string
	conversationPruner          *ConversationPruner
	commandHistory              []string
	historyIndex                int
	historyMu                   sync.Mutex
	pauseState                  *PauseState
	pauseMutex                  sync.Mutex
	traceSession                interface{}
	sessionProvider             api.ClientType
	sessionModel                string
	configOverrides             map[string]interface{}
	currentIteration             int
	sessionIntentEmbedding      []float32
}

// NewAgentStateManager creates a new AgentStateManager with sensible defaults.
func NewAgentStateManager(debug bool) *AgentStateManager {
	return &AgentStateManager{
		messages:                []api.Message{},
		optimizer:               NewConversationOptimizer(true, debug),
		conversationPruner:      NewConversationPruner(debug),
		commandHistory:          []string{},
		historyIndex:            -1,
		activePersona:           "orchestrator",
		circuitBreaker:          &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)},
		falseStopDetectionEnabled: true,
	}
}

// --- Messages ---

func (s *AgentStateManager) GetMessages() []api.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.messages
}

func (s *AgentStateManager) SetMessages(msgs []api.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = msgs
}

func (s *AgentStateManager) AddMessage(msg api.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
}

// --- Session ---

func (s *AgentStateManager) GetSessionID() string {
	return s.sessionID
}

func (s *AgentStateManager) SetSessionID(id string) {
	s.sessionID = id
}

// --- Turn checkpoints ---

func (s *AgentStateManager) GetTurnCheckpoints() []TurnCheckpoint {
	return s.turnCheckpoints
}

func (s *AgentStateManager) SetTurnCheckpoints(cps []TurnCheckpoint) {
	s.turnCheckpoints = cps
}

func (s *AgentStateManager) AddTurnCheckpoint(cp TurnCheckpoint) {
	s.turnCheckpoints = append(s.turnCheckpoints, cp)
}

// --- Checkpoint mutex ---

func (s *AgentStateManager) GetCheckpointMutex() *sync.RWMutex {
	return &s.checkpointMu
}

// --- Summary ---

func (s *AgentStateManager) GetPreviousSummary() string {
	return s.previousSummary
}

func (s *AgentStateManager) SetPreviousSummary(summary string) {
	s.previousSummary = summary
}

// --- Optimizer ---

func (s *AgentStateManager) GetOptimizer() *ConversationOptimizer {
	return s.optimizer
}

func (s *AgentStateManager) SetOptimizer(o *ConversationOptimizer) {
	s.optimizer = o
}

// --- Context tokens ---

func (s *AgentStateManager) GetCurrentContextTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentContextTokens
}

func (s *AgentStateManager) SetCurrentContextTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentContextTokens = n
}

func (s *AgentStateManager) GetMaxContextTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxContextTokens
}

func (s *AgentStateManager) SetMaxContextTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxContextTokens = n
}

// --- Context warning ---

func (s *AgentStateManager) IsContextWarningIssued() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contextWarningIssued
}

func (s *AgentStateManager) SetContextWarningIssued(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextWarningIssued = v
}

// --- Task actions ---
// NOTE: These methods do NOT acquire taskActionsMu internally. Callers that
// need thread-safety must acquire a.state.GetTaskActionsMutex() themselves
// (see state.go:AddTaskAction for the pattern).

func (s *AgentStateManager) GetTaskActions() []TaskAction {
	return s.taskActions
}

func (s *AgentStateManager) SetTaskActions(actions []TaskAction) {
	s.taskActions = actions
}

func (s *AgentStateManager) AddTaskAction(action TaskAction) {
	s.taskActions = append(s.taskActions, action)
}

// --- Task actions mutex ---

func (s *AgentStateManager) GetTaskActionsMutex() *sync.RWMutex {
	return &s.taskActionsMu
}

// --- Cost ---

func (s *AgentStateManager) GetTotalCost() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalCost
}

func (s *AgentStateManager) SetTotalCost(c float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalCost = c
}

func (s *AgentStateManager) AddCost(c float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalCost += c
}

// --- Token counts ---

func (s *AgentStateManager) GetTotalTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalTokens
}

func (s *AgentStateManager) SetTotalTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalTokens = n
}

func (s *AgentStateManager) GetPromptTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.promptTokens
}

func (s *AgentStateManager) SetPromptTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptTokens = n
}

func (s *AgentStateManager) GetCompletionTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.completionTokens
}

func (s *AgentStateManager) SetCompletionTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionTokens = n
}

// --- LLM call tracking ---

func (s *AgentStateManager) GetLLMCallCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.llmCallCount
}

func (s *AgentStateManager) SetLLMCallCount(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmCallCount = n
}

func (s *AgentStateManager) IncrementLLMCallCount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmCallCount++
}

// --- Tool call tracking ---

func (s *AgentStateManager) GetTotalToolCalls() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalToolCalls
}

func (s *AgentStateManager) SetTotalToolCalls(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalToolCalls = n
}

func (s *AgentStateManager) IncrementTotalToolCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalToolCalls++
}

// --- Estimated token responses ---

func (s *AgentStateManager) GetEstimatedTokenResponses() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.estimatedTokenResponses
}

func (s *AgentStateManager) SetEstimatedTokenResponses(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.estimatedTokenResponses = n
}

// --- Cache stats ---

func (s *AgentStateManager) GetCachedTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cachedTokens
}

func (s *AgentStateManager) SetCachedTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedTokens = n
}

func (s *AgentStateManager) GetCachedCostSavings() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cachedCostSavings
}

func (s *AgentStateManager) SetCachedCostSavings(c float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedCostSavings = c
}

// --- Skills and persona ---

func (s *AgentStateManager) GetActiveSkills() []string {
	return s.activeSkills
}

func (s *AgentStateManager) SetActiveSkills(skills []string) {
	s.activeSkills = skills
}

func (s *AgentStateManager) GetActivePersona() string {
	return s.activePersona
}

func (s *AgentStateManager) SetActivePersona(p string) {
	s.activePersona = p
}

// --- Circuit breaker ---

func (s *AgentStateManager) GetCircuitBreaker() *CircuitBreakerState {
	return s.circuitBreaker
}

func (s *AgentStateManager) SetCircuitBreaker(cb *CircuitBreakerState) {
	s.circuitBreaker = cb
}

// --- Tool call guidance ---

func (s *AgentStateManager) IsToolCallGuidanceAdded() bool {
	return s.toolCallGuidanceAdded
}

func (s *AgentStateManager) SetToolCallGuidanceAdded(v bool) {
	s.toolCallGuidanceAdded = v
}

// --- Pending state ---

func (s *AgentStateManager) GetPendingSwitchContextRefresh() string {
	return s.pendingSwitchContextRefresh
}

func (s *AgentStateManager) SetPendingSwitchContextRefresh(v string) {
	s.pendingSwitchContextRefresh = v
}

func (s *AgentStateManager) GetPendingStrictSwitchNotice() string {
	return s.pendingStrictSwitchNotice
}

func (s *AgentStateManager) SetPendingStrictSwitchNotice(v string) {
	s.pendingStrictSwitchNotice = v
}

func (s *AgentStateManager) GetPendingSystemSupplement() string {
	return s.pendingSystemSupplement
}

func (s *AgentStateManager) SetPendingSystemSupplement(v string) {
	s.pendingSystemSupplement = v
}

// --- False stop detection ---

func (s *AgentStateManager) IsFalseStopDetectionEnabled() bool {
	return s.falseStopDetectionEnabled
}

func (s *AgentStateManager) SetFalseStopDetectionEnabled(v bool) {
	s.falseStopDetectionEnabled = v
}

// --- Termination ---

func (s *AgentStateManager) GetLastRunTerminationReason() string {
	return s.lastRunTerminationReason
}

func (s *AgentStateManager) SetLastRunTerminationReason(reason string) {
	s.lastRunTerminationReason = reason
}

// --- Conversation pruner ---

func (s *AgentStateManager) GetConversationPruner() *ConversationPruner {
	return s.conversationPruner
}

func (s *AgentStateManager) SetConversationPruner(pruner *ConversationPruner) {
	s.conversationPruner = pruner
}

// --- Command history ---

func (s *AgentStateManager) GetCommandHistory() []string {
	return s.commandHistory
}

func (s *AgentStateManager) SetCommandHistory(h []string) {
	s.commandHistory = h
}

func (s *AgentStateManager) GetHistoryIndex() int {
	return s.historyIndex
}

func (s *AgentStateManager) SetHistoryIndex(i int) {
	s.historyIndex = i
}

func (s *AgentStateManager) GetHistoryMutex() *sync.Mutex {
	return &s.historyMu
}

// --- Pause ---

func (s *AgentStateManager) GetPauseState() *PauseState {
	return s.pauseState
}

func (s *AgentStateManager) SetPauseState(ps *PauseState) {
	s.pauseState = ps
}

func (s *AgentStateManager) GetPauseMutex() *sync.Mutex {
	return &s.pauseMutex
}

// --- Tracing ---

func (s *AgentStateManager) GetTraceSession() interface{} {
	return s.traceSession
}

func (s *AgentStateManager) SetTraceSession(ts interface{}) {
	s.traceSession = ts
}

// --- Session config ---

func (s *AgentStateManager) GetSessionProvider() api.ClientType {
	return s.sessionProvider
}

func (s *AgentStateManager) SetSessionProvider(ct api.ClientType) {
	s.sessionProvider = ct
}

func (s *AgentStateManager) GetSessionModel() string {
	return s.sessionModel
}

func (s *AgentStateManager) SetSessionModel(m string) {
	s.sessionModel = m
}

// --- Config overrides ---

func (s *AgentStateManager) GetConfigOverrides() map[string]interface{} {
	return s.configOverrides
}

func (s *AgentStateManager) SetConfigOverrides(overrides map[string]interface{}) {
	s.configOverrides = overrides
}

// --- Current iteration ---

func (s *AgentStateManager) GetCurrentIteration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentIteration
}

func (s *AgentStateManager) SetCurrentIteration(iter int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentIteration = iter
}

// --- Session intent embedding ---

func (s *AgentStateManager) GetSessionIntentEmbedding() []float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sessionIntentEmbedding == nil {
		return nil
	}
	// Return a defensive copy
	result := make([]float32, len(s.sessionIntentEmbedding))
	copy(result, s.sessionIntentEmbedding)
	return result
}

func (s *AgentStateManager) SetSessionIntentEmbedding(emb []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emb == nil || len(emb) == 0 {
		s.sessionIntentEmbedding = nil
		return
	}
	// Store a defensive copy
	s.sessionIntentEmbedding = make([]float32, len(emb))
	copy(s.sessionIntentEmbedding, emb)
}

func (s *AgentStateManager) SetSessionIntentEmbeddingIfNil(emb []float32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionIntentEmbedding != nil {
		return false
	}
	if emb == nil || len(emb) == 0 {
		return false
	}
	s.sessionIntentEmbedding = make([]float32, len(emb))
	copy(s.sessionIntentEmbedding, emb)
	return true
}
