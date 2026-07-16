package agent

import (
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// MessageStore manages conversation messages and their timestamps.
type MessageStore interface {
	GetMessages() []api.Message
	SetMessages([]api.Message)
	AddMessage(api.Message)
	GetMessageTimestamps() []time.Time
	SetMessageTimestamps([]time.Time)
}

// SessionStore manages the session identifier.
type SessionStore interface {
	GetSessionID() string
	SetSessionID(string)
}

// CheckpointStore manages turn checkpoints for state persistence.
type CheckpointStore interface {
	GetTurnCheckpoints() []TurnCheckpoint
	SetTurnCheckpoints([]TurnCheckpoint)
	AddTurnCheckpoint(TurnCheckpoint)
	GetCheckpointMutex() *sync.RWMutex
}

// SummaryStore manages the previous conversation summary.
type SummaryStore interface {
	GetPreviousSummary() string
	SetPreviousSummary(string)
}

// OptimizerStore manages the conversation optimizer instance.
type OptimizerStore interface {
	GetOptimizer() *ConversationOptimizer
	SetOptimizer(*ConversationOptimizer)
}

// ContextBudgetStore manages context window token budgeting and warnings.
type ContextBudgetStore interface {
	GetCurrentContextTokens() int
	SetCurrentContextTokens(int)
	GetMaxContextTokens() int
	SetMaxContextTokens(int)
	IsContextWarningIssued() bool
	SetContextWarningIssued(bool)
}

// TaskActionStore manages task actions and their associated mutex.
type TaskActionStore interface {
	GetTaskActions() []TaskAction
	SetTaskActions([]TaskAction)
	AddTaskAction(TaskAction)
	GetTaskActionsMutex() *sync.RWMutex
}

// CostTracker manages billing costs, token costs, and subscription/free token counts.
type CostTracker interface {
	GetTotalCost() float64
	SetTotalCost(float64)
	AddCost(float64)
	AddCostEntry(CostEntry)
	GetChargedCostTotal() float64
	GetTokenCostTotal() float64
	GetSubscriptionTokens() int
	GetFreeTokens() int
	SetChargedCostTotal(float64)
	SetTokenCostTotal(float64)
	SetSubscriptionTokens(int)
	SetFreeTokens(int)
}

// TokenCounter manages prompt and completion token counts.
type TokenCounter interface {
	GetTotalTokens() int
	SetTotalTokens(int)
	GetPromptTokens() int
	SetPromptTokens(int)
	GetCompletionTokens() int
	SetCompletionTokens(int)
}

// LLMCallTracker manages LLM call count tracking.
type LLMCallTracker interface {
	GetLLMCallCount() int
	SetLLMCallCount(int)
	IncrementLLMCallCount()
}

// ToolCallTracker manages tool call count tracking.
type ToolCallTracker interface {
	GetTotalToolCalls() int
	SetTotalToolCalls(int)
	IncrementTotalToolCalls()
}

// CacheStats manages prompt cache statistics.
type CacheStats interface {
	GetCachedTokens() int
	SetCachedTokens(int)
	GetCacheWriteTokens() int
	SetCacheWriteTokens(int)
	GetCachedCostSavings() float64
	SetCachedCostSavings(float64)
	GetImageTokens() int
	SetImageTokens(int)
}

// PersonaStore manages active skills and persona.
type PersonaStore interface {
	GetActiveSkills() []string
	SetActiveSkills([]string)
	GetActivePersona() string
	SetActivePersona(string)
}

// CircuitBreakerStore manages the circuit breaker state.
type CircuitBreakerStore interface {
	GetCircuitBreaker() *CircuitBreakerState
	SetCircuitBreaker(*CircuitBreakerState)
}

// PendingStateStore manages pending state that will be applied on the next turn.
type PendingStateStore interface {
	GetPendingSwitchContextRefresh() string
	SetPendingSwitchContextRefresh(string)
	GetPendingStrictSwitchNotice() string
	SetPendingStrictSwitchNotice(string)
	GetPendingSystemSupplement() string
	SetPendingSystemSupplement(string)
}

// TerminationStore manages the last run termination reason.
type TerminationStore interface {
	GetLastRunTerminationReason() string
	SetLastRunTerminationReason(string)
}

// ConversationPrunerStore manages the conversation pruner instance.
type ConversationPrunerStore interface {
	GetConversationPruner() *ConversationPruner
	SetConversationPruner(*ConversationPruner)
}

// CommandHistoryStore manages command history navigation.
type CommandHistoryStore interface {
	GetCommandHistory() []string
	SetCommandHistory([]string)
	GetHistoryIndex() int
	SetHistoryIndex(int)
	GetHistoryMutex() *sync.Mutex
}

// PauseStore manages pause state and its mutex.
type PauseStore interface {
	GetPauseState() *PauseState
	SetPauseState(*PauseState)
	GetPauseMutex() *sync.Mutex
}

// TraceStore manages the trace session.
type TraceStore interface {
	GetTraceSession() interface{}
	SetTraceSession(interface{})
}

// SessionConfigStore manages per-session provider and model configuration.
type SessionConfigStore interface {
	GetSessionProvider() api.ClientType
	SetSessionProvider(api.ClientType)
	GetSessionModel() string
	SetSessionModel(string)
}

// ConfigOverrideStore manages config overrides for the current session.
type ConfigOverrideStore interface {
	GetConfigOverrides() map[string]interface{}
	SetConfigOverrides(map[string]interface{})
}

// IterationStore manages the current iteration count.
type IterationStore interface {
	GetCurrentIteration() int
	SetCurrentIteration(int)
}

// SessionIntentStore manages the session intent embedding vector.
type SessionIntentStore interface {
	GetSessionIntentEmbedding() []float32
	SetSessionIntentEmbedding([]float32)
	SetSessionIntentEmbeddingIfNil(emb []float32) bool
}

// ProviderErrorStore manages the last provider error information.
type ProviderErrorStore interface {
	GetLastProviderError() *ProviderErrorInfo
	SetLastProviderError(*ProviderErrorInfo)
}

// EstimatedTokenStore manages estimated token response counts.
type EstimatedTokenStore interface {
	GetEstimatedTokenResponses() int
	SetEstimatedTokenResponses(int)
}

// ToolGuidanceStore manages tool call guidance state.
type ToolGuidanceStore interface {
	IsToolCallGuidanceAdded() bool
	SetToolCallGuidanceAdded(bool)
}

// FalseStopStore manages false stop detection enablement.
type FalseStopStore interface {
	IsFalseStopDetectionEnabled() bool
	SetFalseStopDetectionEnabled(bool)
}
