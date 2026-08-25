package agent

import (
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// CostEntry captures a single cost-bearing LLM call with billing-model awareness.
// It carries two cost numbers:
//   - ChargedCost: real USD charged for this call (only > 0 for pay_per_token)
//   - TokenCost: estimated USD value of tokens consumed, from per-model pricing
type CostEntry struct {
	BillingType      string  `json:"billing_type"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
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
