package agent

import (
	"sort"
	"sync"
)

// RoleUsage is the per-role token/cost aggregate for one role:
// the tokens the agent's model calls attributed to that role
// consumed and what they cost. Tokens is PromptTokens+CompletionTokens.
// Exposed via AgentMetricsManager.GetRoleUsage and Agent.GetRoleUsage so
// /usage-style views and embedding surfaces can attribute spend per role.
type RoleUsage struct {
	Role             string  `json:"role"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Tokens           int     `json:"tokens"`
	ChargedCost      float64 `json:"charged_cost"`
	TokenCost        float64 `json:"token_cost"`
	Calls            int     `json:"calls"`
}

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
	// Per-role cost/token accumulator. Keyed by
	// the CostEntry's role; an empty role is bucketed under "unknown".
	roleUsage map[string]*RoleUsage

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

	// Continuation-nudge observation: seed's transient "Please continue…"
	// messages never enter conversation state, so they are invisible in
	// transcripts. Counted at the provider seam and surfaced in snapshot
	// annotations. Monotonic — never reset within a session.
	continuationNudges int

	// CacheStats
	cachedTokens      int
	cacheWriteTokens  int
	cachedCostSavings float64
	// cacheSavingsUnknown is set when at least one cached response had no
	// determinable savings (no actual cost and no usable catalog rate). The
	// cost views render "unknown" instead of a misleading $0 when this is set
	// and no determined savings were recorded.
	cacheSavingsUnknown bool
	imageTokens         int
}

// NewAgentMetricsManager creates a new AgentMetricsManager with zero-initialized fields.
func NewAgentMetricsManager() *AgentMetricsManager {
	return &AgentMetricsManager{roleUsage: make(map[string]*RoleUsage)}
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
	m.addCostEntryRole(entry)
}

// addCostEntryRole rolls one cost entry into the per-role accumulator.
// The caller must hold m.mu (AddCostEntry
// does). The per-role ChargedCost/TokenCost use the same > 0 guards as the
// aggregate totals, so the per-role sums agree with the aggregate sums.
func (m *AgentMetricsManager) addCostEntryRole(entry CostEntry) {
	role := entry.Role
	if role == "" {
		role = "unknown"
	}
	if m.roleUsage == nil {
		m.roleUsage = make(map[string]*RoleUsage)
	}
	stat, ok := m.roleUsage[role]
	if !ok {
		stat = &RoleUsage{Role: role}
		m.roleUsage[role] = stat
	}
	stat.PromptTokens += entry.PromptTokens
	stat.CompletionTokens += entry.CompletionTokens
	stat.Tokens += entry.PromptTokens + entry.CompletionTokens
	if entry.ChargedCost > 0 {
		stat.ChargedCost += entry.ChargedCost
	}
	if entry.TokenCost > 0 {
		stat.TokenCost += entry.TokenCost
	}
	stat.Calls++
}

// GetRoleUsage returns the per-role token/cost totals, sorted by role for
// stable output. A nil receiver returns nil. The per-role ChargedCost/
// TokenCost sums agree with the aggregate chargedCostTotal/tokenCostTotal
// because both are accumulated with the same guards.
func (m *AgentMetricsManager) GetRoleUsage() []RoleUsage {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RoleUsage, 0, len(m.roleUsage))
	for _, s := range m.roleUsage {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out
}

// SetRoleUsage replaces the per-role usage map.
// Used by state restore to rehydrate the per-role totals so they keep summing
// to the restored overall totals. An empty/nil slice clears the map.
func (m *AgentMetricsManager) SetRoleUsage(usages []RoleUsage) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roleUsage = make(map[string]*RoleUsage, len(usages))
	for _, ru := range usages {
		m.roleUsage[ru.Role] = &RoleUsage{
			Role:             ru.Role,
			PromptTokens:     ru.PromptTokens,
			CompletionTokens: ru.CompletionTokens,
			Tokens:           ru.Tokens,
			ChargedCost:      ru.ChargedCost,
			TokenCost:        ru.TokenCost,
			Calls:            ru.Calls,
		}
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

// Continuation-nudge observation (see field comment).
func (m *AgentMetricsManager) RecordContinuationNudges(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.continuationNudges += n
}

func (m *AgentMetricsManager) GetContinuationNudges() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.continuationNudges
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

// GetCacheSavingsUnknown reports whether any cached response had no
// determinable savings (missing actual cost and missing catalog rate).
func (m *AgentMetricsManager) GetCacheSavingsUnknown() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cacheSavingsUnknown
}

// SetCacheSavingsUnknown records that a cached response had no determinable
// savings. Monotonic within a session — once unknown, the session's savings
// display stays "unknown" until a determined value is set.
func (m *AgentMetricsManager) SetCacheSavingsUnknown(unknown bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheSavingsUnknown = unknown
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
