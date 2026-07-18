# SP-113: Multi-Billing-Model Cost Tracking

**Status:** 🟢 Implemented — Phases 1–4 shipped (`4552363c` 2026-07-02, originally numbered SP-080 before renumber). Post-merge cleanup (`bab487da` 2026-07-16): subagent double-debit fix, fleet budget isolation, CLI footer annotations, ProviderTable billing column. Spec retained at root as living reference for future scope (subscription quota tracking, per-billing-type cost alerts, Ollama Cloud credits).

> **Note:** Renumbered from SP-080 (2026-07-05) to resolve a number collision with
> the completed SP-080 "Type the Unknown-Tool Error" spec in `_completed/`.

## Problem

Sprout supports providers with fundamentally different billing models, but the
cost tracking system treats every provider identically via a single scalar
`float64` cost. This causes three concrete failures:

### 1. Subscription providers show misleading $0.00

Providers like ZAI Coding Plan (GLM Coding Plan) charge a flat monthly fee.
Their API responses report no cost field, so `accumulateResponseCost` adds $0.
Users see "$0.0000" and can't tell whether usage is genuinely free, included in
a subscription, or broken. Token counts are tracked but the value of those
tokens is invisible.

### 2. Fleet USD budgets miscount mixed workflows

The fleet USD budget (`FleetUsdBudget.Add`) debits any cost > 0. But
subscription providers always report $0, so a subagent running on ZAI Coding
Plan doesn't consume any fleet budget — even though it's consuming a
rate-limited resource. Conversely, providers that report $0 because the API
doesn't include a cost field (DeepSeek, OpenAI) get under-counted until the
runtime cost estimation fix (which estimates from pricing) fills the gap.

### 3. No way to show "what am I actually spending?"

The costs page (`/api/costs/summary`) aggregates a single `total_cost` number.
There's no breakdown by billing model — the user can't see "I spent $5.37 on
API calls, consumed 45K subscription tokens, and used 12K free local tokens."
This makes it impossible to answer basic questions: "Am I getting my money's
worth from the subscription?" or "Should I route subagents to the subscription
provider to save API costs?"

## Provider landscape

| Provider | Config billing | Actual model | Why it breaks today |
|----------|:---:|---|---|
| DeepSeek | `pay_per_token` | Per-token API | API doesn't report cost in response (fixed by runtime estimation) |
| DeepInfra | `pay_per_token` | Per-token API | Reports cost in response (`usage.estimated_cost`) |
| OpenRouter | `pay_per_token` | Per-token API | Reports cost in response (`usage.cost`) |
| OpenAI | `pay_per_token` | Per-token API | API doesn't report cost; needs estimation |
| ZAI Coding Plan | `subscription` | Flat monthly fee | $0 reported; tokens tracked but value invisible |
| ZAI (general) | `pay_per_token` | Per-token API | Pricing in `model_info` only |
| MiniMax | `pay_per_token` | Per-token API | Pricing in `model_info` |
| Ollama Local | `free` | Self-hosted | $0, no marginal cost |
| LM Studio | `free` | Self-hosted | $0, no marginal cost |
| Ollama Cloud | `pay_per_token` | Credit-based | Pricing not yet populated |
| Cerebras | `free` | Free tier | $0, may have rate limits |

### Mixed-workflow scenarios

These all happen today:

```
A. Primary: DeepSeek (pay-per-token) + Subagents: ZAI Coding Plan (subscription)
B. Primary: ZAI Coding Plan (subscription) + Subagents: Ollama local (free)
C. Primary: OpenRouter (pay-per-token) + Subagents: DeepInfra (pay-per-token) + Ollama (free)
```

In scenario A, the fleet budget should only count the primary's DeepSeek cost —
the ZAI subagent usage is "included." In scenario C, the budget should count
both pay-per-token providers but not Ollama.

## Design

### Core concept: two cost numbers

Every cost-bearing event carries **two** cost values:

- **ChargedCost** — real USD actually charged for this call. Only > 0 for
  `pay_per_token` providers. Always 0 for `subscription` and `free`.
- **TokenCost** — estimated USD *value* of the tokens consumed, computed from
  per-model pricing regardless of billing model. For pay-per-token this equals
  ChargedCost. For subscription, it's the "you got $X worth of tokens for free"
  number. For free/local, it's 0 (no pricing data).

This dual-cost model is the foundation: it lets the dashboard show both "what
you paid" and "what you consumed" without conflating them.

### Layer 1: Provider billing-type tag

Add `billing_type` to provider config (`pkg/agent_providers/provider_config.go`):

```go
const (
    BillingPayPerToken  = "pay_per_token"  // default — real USD per token
    BillingSubscription = "subscription"   // flat-rate, quota/rate-limited
    BillingFree         = "free"           // self-hosted, zero marginal cost
)

type ProviderConfig struct {
    // ...existing fields...
    BillingType string `json:"billing_type,omitempty"`
}

func (c *ProviderConfig) BillingTypeResolved() string {
    if c.BillingType != "" {
        return c.BillingType
    }
    endpoint := strings.ToLower(c.Endpoint)
    if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
        return BillingFree
    }
    if c.Name == "zai-coding" {
        return BillingSubscription
    }
    return BillingPayPerToken
}
```

JSON configs get explicit tags where needed:
```json
// zai-coding.json
{ "billing_type": "subscription" }
// lmstudio.json — auto-detected free via localhost
// deepseek.json — no change (defaults to pay_per_token)
```

Custom providers get a `billing_type` field in the custom provider form.

### Layer 2: CostEntry struct

New file `pkg/agent/cost_model.go`:

```go
type CostEntry struct {
    BillingType      string  `json:"billing_type"`
    Provider         string  `json:"provider"`
    Model            string  `json:"model"`
    ChargedCost      float64 `json:"charged_cost"`
    TokenCost        float64 `json:"token_cost,omitempty"`
    PromptTokens     int     `json:"prompt_tokens"`
    CompletionTokens int     `json:"completion_tokens"`
    CachedTokens     int     `json:"cached_tokens,omitempty"`
}
```

### Layer 3: State manager changes

`AgentStateManager` gains parallel counters alongside the existing scalar:

```go
type AgentStateManager struct {
    // ...existing fields...
    totalCost         float64 // backward-compat: equals chargedCostTotal
    chargedCostTotal  float64 // sum of ChargedCost (pay-per-token only)
    tokenCostTotal    float64 // sum of TokenCost (all providers, estimated value)
    subscriptionTokens int    // tokens consumed on subscription providers
    freeTokens         int    // tokens consumed on free providers
}
```

- `AddCost(float64)` stays (backward-compat, adds to chargedCostTotal)
- `AddCostEntry(CostEntry)` is new — routes to the right counters based on billing type

### Layer 4: Fleet budget integration

`FleetUsdBudget.Add()` only debits `ChargedCost`, not `TokenCost`. This means:
- A $5 fleet budget with a ZAI Coding Plan subagent isn't consumed by subscription usage
- The same budget IS consumed by DeepSeek/OpenRouter API calls
- Mixed workflows get correct budget enforcement

No change to `FleetUsdBudget` itself — the caller just passes `ChargedCost`
instead of the raw estimated cost.

### Layer 5: Cost history records

`CostRecord` in `pkg/webui/cost_tracking.go` gains new fields:

```go
type CostRecord struct {
    // ...existing fields...
    BillingType string  `json:"billing_type,omitempty"`
    ChargedCost float64 `json:"charged_cost,omitempty"`
    TokenCost   float64 `json:"token_cost,omitempty"`
}
```

Old records (missing these fields) default to `billing_type: "pay_per_token"`
and `charged_cost = cost` — fully backward compatible.

### Layer 6: API + UI

`/api/costs/summary` gains billing-type breakdowns:

```json
{
  "total_cost": 5.37,
  "charged_cost": 5.37,
  "token_value": 8.42,
  "by_billing_type": {
    "pay_per_token": { "cost": 5.37, "tokens": 1200000 },
    "subscription": { "cost": 0.0, "tokens": 45000 },
    "free": { "cost": 0.0, "tokens": 12000 }
  },
  "by_provider": { ... },
  "by_model": { ... }
}
```

The costs page shows a breakdown:

```
┌─────────────────────────────────────────────┐
│  Total API Spend:         $5.37             │
│  Token Value Consumed:    $8.42             │
│  ───────────────────────────────────────    │
│  Pay-per-token:           $5.37 (1.2M tok)  │
│  Subscription (included): $0.00 (45K tok)   │
│  Local/Free:              $0.00 (12K tok)   │
└─────────────────────────────────────────────┘
```

The footer/inline summary annotates:
- Pay-per-token: `$0.0023`
- Subscription: `included`
- Free: `free`
- Mixed: `$0.0023 · 12K free`

### Layer 7: Runtime wiring

`accumulateResponseCost` in `seed_provider.go` resolves the billing type from
the provider config and constructs a `CostEntry`:

```go
func (sp *sproutProvider) accumulateResponseCost(resp *core.ChatResponse) {
    billingType := sp.resolveBillingType()
    chargedCost := api.UsageCost(resp.Usage)
    if chargedCost == 0 && billingType == BillingPayPerToken && resp.Usage.TotalTokens > 0 {
        chargedCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
    }
    var tokenCost float64
    if billingType == BillingSubscription || billingType == BillingFree {
        tokenCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
    }
    entry := CostEntry{
        BillingType:      billingType,
        ChargedCost:      chargedCost,
        TokenCost:        tokenCost,
        PromptTokens:     resp.Usage.PromptTokens,
        CompletionTokens: resp.Usage.CompletionTokens,
    }
    sp.agent.state.AddCostEntry(entry)
}
```

## Implementation phases

### Phase 1: Foundation (Go backend)
- [ ] Add `billing_type` field to `ProviderConfig` + `BillingTypeResolved()` method
- [ ] Set `billing_type` in zai-coding.json config
- [ ] Create `pkg/agent/cost_model.go` with `CostEntry` struct
- [ ] Add `chargedCostTotal`, `tokenCostTotal`, `subscriptionTokens`, `freeTokens` to `AgentStateManager`
- [ ] Add `AddCostEntry(CostEntry)` method on `AgentStateManager`
- [ ] Wire `accumulateResponseCost` to construct `CostEntry` with billing type
- [ ] Update `FleetUsdBudget` callers to pass `ChargedCost` only
- [ ] Update `AgentState` (persistence) with new fields
- [ ] Tests: billing-type resolution, cost entry routing, fleet budget isolation

### Phase 2: Persistence + API
- [ ] Add `BillingType`, `ChargedCost`, `TokenCost` to `CostRecord`
- [ ] Update `CostStore.RecordCost` to accept billing-type data
- [ ] Update `/api/costs/summary` to include `by_billing_type` breakdown
- [ ] Backward-compat: old records default to `pay_per_token`
- [ ] Tests: cost record round-trip, summary aggregation by billing type

### Phase 3: UI
- [ ] Update `CostSummary` TypeScript type with billing-type fields
- [ ] Add billing-type breakdown section to `CostsPage.tsx`
- [ ] Update footer/inline summary to annotate subscription/free
- [ ] Add "Token Value" card alongside "Total Spend"
- [ ] Tests: summary card rendering with mixed billing types

### Phase 4: Polish + custom providers
- [ ] Auto-detect billing type for custom providers (localhost → free)
- [ ] Add `billing_type` selector to custom provider form in webui
- [ ] Add billing-type column to provider table in costs page
- [ ] Documentation: explain the three billing models in help text

## Decisions

- **Two cost numbers, not one.** `ChargedCost` (what you paid) and `TokenCost`
  (what tokens were worth) serve different audiences: budget enforcement needs
  charged cost; ROI analysis needs token cost. Collapsing to one loses
  information.
- **Provider config is the source of truth for billing type.** Not the API
  response, not heuristics — the config is the declared contract. Auto-detection
  (localhost → free) is a fallback, not the primary path.
- **Backward-compatible scalar.** `totalCost float64` stays as the sum of
  charged costs. Old code, old sessions, old API consumers all keep working.
  The new fields are additive.
- **Fleet budget only debits charged cost.** A subscription subagent running
  thousands of tokens should not eat a $5 fleet budget — there's no marginal
  spend to protect against.

## Future considerations

- **Subscription quota tracking**: Once we know a provider is subscription-based,
  we could track remaining quota/rate-limit usage (if the provider exposes it)
  and surface "ZAI Coding Plan: 45K/500K daily tokens used."
- **Provider-specific cost APIs**: Some providers (OpenAI, DeepSeek) have
  separate billing APIs that report actual account spend. A future phase could
  reconcile our per-call estimates against the provider's monthly invoice.
- **Cost alerts by billing type**: "You've spent $10 on API calls today" vs
  "Your subscription usage is approaching its daily limit."
- **Ollama Cloud credits**: Ollama Cloud uses a credit-based system that's
  neither pure pay-per-token nor subscription. The `billing_type` field could
  gain a `credits` value, or the `TokenCost` field could track credits instead
  of USD for credit-based providers.
