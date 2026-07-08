package agent

import (
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// StateManager defines the interface for managing conversation state
// previously held directly in the Agent struct.
// It is composed of focused sub-interfaces defined in state_interfaces.go.
type StateManager interface {
	MessageStore
	SessionStore
	CheckpointStore
	SummaryStore
	OptimizerStore
	ContextBudgetStore
	TaskActionStore
	CostTracker
	TokenCounter
	LLMCallTracker
	ToolCallTracker
	CacheStats
	PersonaStore
	CircuitBreakerStore
	PendingStateStore
	TerminationStore
	ConversationPrunerStore
	CommandHistoryStore
	PauseStore
	TraceStore
	SessionConfigStore
	ConfigOverrideStore
	IterationStore
	SessionIntentStore
	ProviderErrorStore
	EstimatedTokenStore
	ToolGuidanceStore
	FalseStopStore
}

// AgentStateManager implements StateManager with simple field-backed getters/setters.
type AgentStateManager struct {
	// General mutex for protecting most shared state
	mu sync.RWMutex

	messages                    []api.Message
	messageTimestamps           []time.Time
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
	chargedCostTotal            float64 // sum of ChargedCost (pay-per-token only)
	tokenCostTotal              float64 // sum of TokenCost (all providers, estimated value)
	subscriptionTokens          int     // tokens consumed on subscription providers
	freeTokens                  int     // tokens consumed on free providers
	totalTokens                 int
	promptTokens                int
	completionTokens            int
	llmCallCount                int
	totalToolCalls              int
	estimatedTokenResponses     int
	cachedTokens                int
	cacheWriteTokens            int
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
	currentIteration            int
	sessionIntentEmbedding      []float32
	lastProviderError           *ProviderErrorInfo
}

// NewAgentStateManager creates a new AgentStateManager with sensible defaults.
func NewAgentStateManager(debug bool) *AgentStateManager {
	return &AgentStateManager{
		messages:                  []api.Message{},
		optimizer:                 NewConversationOptimizer(true, debug),
		conversationPruner:        NewConversationPruner(debug),
		commandHistory:            []string{},
		historyIndex:              -1,
		activePersona:             personas.IDOrchestrator,
		circuitBreaker:            &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)},
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
	// Rebuild timestamps to match the new message count. Existing
	// timestamps are preserved for messages that remain; new messages
	// (if the slice grew) get the current time.
	if len(s.messageTimestamps) < len(msgs) {
		now := time.Now()
		for i := len(s.messageTimestamps); i < len(msgs); i++ {
			s.messageTimestamps = append(s.messageTimestamps, now)
		}
	} else if len(s.messageTimestamps) > len(msgs) {
		s.messageTimestamps = s.messageTimestamps[:len(msgs)]
	}
}

// GetMessageTimestamps returns the creation timestamps for each message.
func (s *AgentStateManager) GetMessageTimestamps() []time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.messageTimestamps
}

// SetMessageTimestamps sets the creation timestamps for each message.
func (s *AgentStateManager) SetMessageTimestamps(ts []time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messageTimestamps = ts
}

func (s *AgentStateManager) AddMessage(msg api.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	s.messageTimestamps = append(s.messageTimestamps, time.Now())
}

// --- Session ---

// Session ID accessors hold s.mu — autoSaveState (state.go) writes
// sessionID from the main flow while RecordTurnCheckpointAsync
// (turn_checkpoints.go) reads it from a background goroutine. The
// race detector caught this on TestRunSeamlessPlanning_ContextCancelled.

func (s *AgentStateManager) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func (s *AgentStateManager) SetSessionID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// AddCostEntry routes a billing-aware cost entry to the correct counters.
// chargedCostTotal always mirrors totalCost for backward compatibility.
func (s *AgentStateManager) AddCostEntry(entry CostEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ChargedCost > 0 {
		s.chargedCostTotal += entry.ChargedCost
		s.totalCost += entry.ChargedCost // backward-compat: totalCost = sum of charged costs
	}
	if entry.TokenCost > 0 {
		s.tokenCostTotal += entry.TokenCost
	}
	tokens := entry.PromptTokens + entry.CompletionTokens
	switch entry.BillingType {
	case BillingSubscription:
		s.subscriptionTokens += tokens
	case BillingFree:
		s.freeTokens += tokens
	}
}

func (s *AgentStateManager) GetChargedCostTotal() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chargedCostTotal
}

func (s *AgentStateManager) GetTokenCostTotal() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokenCostTotal
}

func (s *AgentStateManager) GetSubscriptionTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscriptionTokens
}

func (s *AgentStateManager) GetFreeTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.freeTokens
}

func (s *AgentStateManager) SetChargedCostTotal(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chargedCostTotal = v
}

func (s *AgentStateManager) SetTokenCostTotal(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenCostTotal = v
}

func (s *AgentStateManager) SetSubscriptionTokens(v int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptionTokens = v
}

func (s *AgentStateManager) SetFreeTokens(v int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freeTokens = v
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

func (s *AgentStateManager) GetCacheWriteTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cacheWriteTokens
}

func (s *AgentStateManager) SetCacheWriteTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheWriteTokens = n
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionProvider
}

func (s *AgentStateManager) SetSessionProvider(ct api.ClientType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionProvider = ct
}

func (s *AgentStateManager) GetSessionModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionModel
}

func (s *AgentStateManager) SetSessionModel(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// GetLastProviderError returns the last provider error info
func (s *AgentStateManager) GetLastProviderError() *ProviderErrorInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastProviderError
}

// SetLastProviderError sets the last provider error info
func (s *AgentStateManager) SetLastProviderError(err *ProviderErrorInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastProviderError = err
}
