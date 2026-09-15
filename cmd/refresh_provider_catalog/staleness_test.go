package main

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// TestModelListed covers the membership check the staleness policy relies on:
// a recommendation must count as stale both when it vanished from the live
// model list and when it was never set.
func TestModelListed(t *testing.T) {
	models := []providercatalog.Model{
		{ID: "deepseek-ai/DeepSeek-V4-Flash-0731"},
		{ID: "deepseek-ai/DeepSeek-V4-Pro"},
	}
	cases := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"listed", "deepseek-ai/DeepSeek-V4-Flash-0731", true},
		{"case-insensitive", "DEEPSEEK-AI/DeepSeek-V4-Flash-0731", true},
		{"missing", "deepseek-ai/DeepSeek-V3.1-Terminus", false},
		{"empty id", "", false},
		{"whitespace id", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelListed(tc.modelID, models); got != tc.want {
				t.Errorf("modelListed(%q) = %v, want %v", tc.modelID, got, tc.want)
			}
		})
	}
}

// TestModelPricingState covers the zombie-recommendation signal: a model
// still listed but missing pricing while siblings report it is usually a
// deprecation in progress.
func TestModelPricingState(t *testing.T) {
	models := []providercatalog.Model{
		{ID: "old/Terminus"},
		{ID: "new/V4", InputCost: 0.06, OutputCost: 0.18},
	}
	hasPricing, hasAnyPricing := modelPricingState("old/Terminus", models)
	if hasPricing {
		t.Error("expected old/Terminus to have no pricing")
	}
	if !hasAnyPricing {
		t.Error("expected sibling models to have pricing")
	}

	// All models without pricing → no signal (provider-wide gap, not zombie).
	hasPricing, hasAnyPricing = modelPricingState("old/Terminus", []providercatalog.Model{{ID: "old/Terminus"}})
	if hasPricing || hasAnyPricing {
		t.Errorf("expected no pricing signal for a provider-wide gap, got hasPricing=%v hasAnyPricing=%v", hasPricing, hasAnyPricing)
	}
}
