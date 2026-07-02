package agent

import (
	"math"
	"testing"
)

// floatEq checks two floats are within tolerance
func floatEq(got, want float64, tol float64) bool {
	return math.Abs(got-want) < tol
}

func TestAddCostEntry_PayPerToken(t *testing.T) {
	sm := NewAgentStateManager(false)

	entry := CostEntry{
		BillingType:      BillingPayPerToken,
		Provider:         "openai",
		Model:            "gpt-4",
		ChargedCost:      0.10,
		TokenCost:        0.10,
		PromptTokens:     1000,
		CompletionTokens: 500,
	}
	sm.AddCostEntry(entry)

	if got := sm.GetChargedCostTotal(); !floatEq(got, 0.10, 0.0001) {
		t.Errorf("GetChargedCostTotal() = %f, want 0.10", got)
	}
	if got := sm.GetTokenCostTotal(); !floatEq(got, 0.10, 0.0001) {
		t.Errorf("GetTokenCostTotal() = %f, want 0.10", got)
	}
	if got := sm.GetTotalCost(); !floatEq(got, 0.10, 0.0001) {
		t.Errorf("GetTotalCost() = %f, want 0.10", got)
	}
	if got := sm.GetSubscriptionTokens(); got != 0 {
		t.Errorf("GetSubscriptionTokens() = %d, want 0", got)
	}
	if got := sm.GetFreeTokens(); got != 0 {
		t.Errorf("GetFreeTokens() = %d, want 0", got)
	}
}

func TestAddCostEntry_Subscription(t *testing.T) {
	sm := NewAgentStateManager(false)

	entry := CostEntry{
		BillingType:      BillingSubscription,
		Provider:         "zai-coding",
		Model:            "glm-4",
		ChargedCost:      0,
		TokenCost:        0.05,
		PromptTokens:     2000,
		CompletionTokens: 1000,
	}
	sm.AddCostEntry(entry)

	if got := sm.GetChargedCostTotal(); got != 0.0 {
		t.Errorf("GetChargedCostTotal() = %f, want 0.0", got)
	}
	if got := sm.GetTokenCostTotal(); !floatEq(got, 0.05, 0.0001) {
		t.Errorf("GetTokenCostTotal() = %f, want 0.05", got)
	}
	if got := sm.GetSubscriptionTokens(); got != 3000 {
		t.Errorf("GetSubscriptionTokens() = %d, want 3000", got)
	}
	if got := sm.GetFreeTokens(); got != 0 {
		t.Errorf("GetFreeTokens() = %d, want 0", got)
	}
}

func TestAddCostEntry_Free(t *testing.T) {
	sm := NewAgentStateManager(false)

	entry := CostEntry{
		BillingType:      BillingFree,
		Provider:         "local",
		Model:            "llama-3",
		ChargedCost:      0,
		TokenCost:        0.02,
		PromptTokens:     500,
		CompletionTokens: 250,
	}
	sm.AddCostEntry(entry)

	if got := sm.GetChargedCostTotal(); got != 0.0 {
		t.Errorf("GetChargedCostTotal() = %f, want 0.0", got)
	}
	if got := sm.GetTokenCostTotal(); !floatEq(got, 0.02, 0.0001) {
		t.Errorf("GetTokenCostTotal() = %f, want 0.02", got)
	}
	if got := sm.GetSubscriptionTokens(); got != 0 {
		t.Errorf("GetSubscriptionTokens() = %d, want 0", got)
	}
	if got := sm.GetFreeTokens(); got != 750 {
		t.Errorf("GetFreeTokens() = %d, want 750", got)
	}
}

func TestAddCostEntry_MultipleAccumulate(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Pay-per-token entry
	sm.AddCostEntry(CostEntry{
		BillingType:      BillingPayPerToken,
		ChargedCost:      0.10,
		TokenCost:        0.10,
		PromptTokens:     1000,
		CompletionTokens: 500,
	})

	// Subscription entry
	sm.AddCostEntry(CostEntry{
		BillingType:      BillingSubscription,
		ChargedCost:      0,
		TokenCost:        0.05,
		PromptTokens:     2000,
		CompletionTokens: 1000,
	})

	// Free entry
	sm.AddCostEntry(CostEntry{
		BillingType:      BillingFree,
		ChargedCost:      0,
		TokenCost:        0.02,
		PromptTokens:     500,
		CompletionTokens: 250,
	})

	// Another pay-per-token entry
	sm.AddCostEntry(CostEntry{
		BillingType:      BillingPayPerToken,
		ChargedCost:      0.20,
		TokenCost:        0.20,
		PromptTokens:     3000,
		CompletionTokens: 1500,
	})

	if got := sm.GetChargedCostTotal(); !floatEq(got, 0.30, 0.0001) {
		t.Errorf("GetChargedCostTotal() = %f, want 0.30", got)
	}
	if got := sm.GetTokenCostTotal(); !floatEq(got, 0.37, 0.0001) {
		t.Errorf("GetTokenCostTotal() = %f, want 0.37", got)
	}
	if got := sm.GetTotalCost(); !floatEq(got, 0.30, 0.0001) {
		t.Errorf("GetTotalCost() = %f, want 0.30", got)
	}
	if got := sm.GetSubscriptionTokens(); got != 3000 {
		t.Errorf("GetSubscriptionTokens() = %d, want 3000", got)
	}
	if got := sm.GetFreeTokens(); got != 750 {
		t.Errorf("GetFreeTokens() = %d, want 750", got)
	}
}

func TestAddCostEntry_ZeroCosts(t *testing.T) {
	sm := NewAgentStateManager(false)

	entry := CostEntry{
		BillingType:      BillingPayPerToken,
		ChargedCost:      0,
		TokenCost:        0,
		PromptTokens:     100,
		CompletionTokens: 50,
	}
	sm.AddCostEntry(entry)

	// When ChargedCost is 0, totalCost and chargedCostTotal should not change.
	if got := sm.GetChargedCostTotal(); got != 0.0 {
		t.Errorf("GetChargedCostTotal() = %f, want 0.0", got)
	}
	if got := sm.GetTotalCost(); got != 0.0 {
		t.Errorf("GetTotalCost() = %f, want 0.0", got)
	}
	if got := sm.GetTokenCostTotal(); got != 0.0 {
		t.Errorf("GetTokenCostTotal() = %f, want 0.0", got)
	}
}

func TestAddCostEntry_Setters(t *testing.T) {
	sm := NewAgentStateManager(false)

	sm.SetChargedCostTotal(1.00)
	sm.SetTokenCostTotal(2.00)
	sm.SetSubscriptionTokens(5000)
	sm.SetFreeTokens(3000)

	if got := sm.GetChargedCostTotal(); !floatEq(got, 1.00, 0.0001) {
		t.Errorf("GetChargedCostTotal() = %f, want 1.00", got)
	}
	if got := sm.GetTokenCostTotal(); !floatEq(got, 2.00, 0.0001) {
		t.Errorf("GetTokenCostTotal() = %f, want 2.00", got)
	}
	if got := sm.GetSubscriptionTokens(); got != 5000 {
		t.Errorf("GetSubscriptionTokens() = %d, want 5000", got)
	}
	if got := sm.GetFreeTokens(); got != 3000 {
		t.Errorf("GetFreeTokens() = %d, want 3000", got)
	}
}
