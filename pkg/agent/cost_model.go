package agent

import (
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// CostEntry captures a single cost-bearing LLM call with billing-model
// awareness. It carries two cost numbers:
//   - ChargedCost: real USD charged for this call (only > 0 for pay_per_token)
//   - TokenCost: estimated USD value of tokens consumed, from per-model pricing
//
// Role is the role name (a configuration.Role* constant such as
// "coder" or "reviewer") whose model selection drove this call. The metrics
// manager aggregates per-role usage keyed by this field; an empty Role is
// bucketed under "unknown" (consistent with the language-guard metric), so a
// call that never resolved a role never drops its usage.
type CostEntry struct {
	BillingType      string  `json:"billing_type"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Role             string  `json:"role,omitempty"`
	ChargedCost      float64 `json:"charged_cost"`
	TokenCost        float64 `json:"token_cost,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	ImageTokens      int     `json:"image_tokens,omitempty"`
}

// Billing type constants (re-exported for convenience within the agent package).
const (
	BillingPayPerToken  = providers.BillingPayPerToken
	BillingSubscription = providers.BillingSubscription
	BillingFree         = providers.BillingFree
)
