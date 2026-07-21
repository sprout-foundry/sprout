package agent

import "sync"

// AgentMetricsManager owns 6 sub-interfaces: CostTracker, TokenCounter,
// LLMCallTracker, ToolCallTracker, CacheStats, and EstimatedTokenStore.
// All fields are protected by a single RWMutex.
type AgentMetricsManager struct {
	mu sync.RWMutex

	// CostTracker
	totalCost          float64
	chargedCostTotal   float64
	tokenCostTotal     float64
	subscriptionTokens int
	freeTokens         int

	// TokenCounter
	totalTokens      int
	promptTokens     int
	completionTokens int

	// LLMCallTracker
	llmCallCount int

	// ToolCallTracker
	totalToolCalls int

	// EstimatedTokenStore
	estimatedTokenResponses int

	// CacheStats
	cachedTokens      int
	cacheWriteTokens  int
	cachedCostSavings float64
	imageTokens       int
}

// NewAgentMetricsManager creates a new AgentMetricsManager with zero-initialized fields.
func NewAgentMetricsManager() *AgentMetricsManager {
	return &AgentMetricsManager{}
}

// CostTracker
func (m *AgentMetricsManager) GetTotalCost() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalCost
}
func (m *AgentMetricsManager) SetTotalCost(c float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalCost = c
}
func (m *AgentMetricsManager) AddCost(c float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalCost += c
}
func (m *AgentMetricsManager) AddCostEntry(entry CostEntry) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.ChargedCost > 0 {
		m.chargedCostTotal += entry.ChargedCost
		m.totalCost += entry.ChargedCost
	}
	if entry.TokenCost > 0 {
		m.tokenCostTotal += entry.TokenCost
	}
	tokens := entry.PromptTokens + entry.CompletionTokens
	switch entry.BillingType {
	case BillingSubscription:
		m.subscriptionTokens += tokens
	case BillingFree:
		m.freeTokens += tokens
	}
}
func (m *AgentMetricsManager) GetChargedCostTotal() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.chargedCostTotal
}
func (m *AgentMetricsManager) SetChargedCostTotal(v float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chargedCostTotal = v
}
func (m *AgentMetricsManager) GetTokenCostTotal() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tokenCostTotal
}
func (m *AgentMetricsManager) SetTokenCostTotal(v float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenCostTotal = v
}
func (m *AgentMetricsManager) GetSubscriptionTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subscriptionTokens
}
func (m *AgentMetricsManager) SetSubscriptionTokens(v int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptionTokens = v
}
func (m *AgentMetricsManager) GetFreeTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.freeTokens
}
func (m *AgentMetricsManager) SetFreeTokens(v int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.freeTokens = v
}

// TokenCounter
func (m *AgentMetricsManager) GetTotalTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalTokens
}
func (m *AgentMetricsManager) SetTotalTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalTokens = n
}
func (m *AgentMetricsManager) GetPromptTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.promptTokens
}
func (m *AgentMetricsManager) SetPromptTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.promptTokens = n
}
func (m *AgentMetricsManager) GetCompletionTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.completionTokens
}
func (m *AgentMetricsManager) SetCompletionTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completionTokens = n
}

// LLMCallTracker
func (m *AgentMetricsManager) GetLLMCallCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.llmCallCount
}
func (m *AgentMetricsManager) SetLLMCallCount(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.llmCallCount = n
}
func (m *AgentMetricsManager) IncrementLLMCallCount() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.llmCallCount++
}

// ToolCallTracker
func (m *AgentMetricsManager) GetTotalToolCalls() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalToolCalls
}
func (m *AgentMetricsManager) SetTotalToolCalls(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalToolCalls = n
}
func (m *AgentMetricsManager) IncrementTotalToolCalls() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalToolCalls++
}

// EstimatedTokenStore
func (m *AgentMetricsManager) GetEstimatedTokenResponses() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.estimatedTokenResponses
}
func (m *AgentMetricsManager) SetEstimatedTokenResponses(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.estimatedTokenResponses = n
}

// CacheStats
func (m *AgentMetricsManager) GetCachedTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedTokens
}
func (m *AgentMetricsManager) SetCachedTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedTokens = n
}
func (m *AgentMetricsManager) GetCacheWriteTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cacheWriteTokens
}
func (m *AgentMetricsManager) SetCacheWriteTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheWriteTokens = n
}
func (m *AgentMetricsManager) GetCachedCostSavings() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedCostSavings
}
func (m *AgentMetricsManager) SetCachedCostSavings(c float64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedCostSavings = c
}
func (m *AgentMetricsManager) GetImageTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.imageTokens
}
func (m *AgentMetricsManager) SetImageTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.imageTokens = n
}
