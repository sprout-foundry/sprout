package cmd

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestResolvePreferredCustomProviderModelAllowsNumericSelection(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
		{ID: "qwen3.5-35-A3B"},
	}

	selected, err := resolvePreferredCustomProviderModel("2", models)
	if err != nil {
		t.Fatalf("expected numeric selection to succeed, got error: %v", err)
	}
	if selected != "qwen3.5-35-A3B" {
		t.Fatalf("expected second discovered model, got %q", selected)
	}
}

func TestResolvePreferredCustomProviderModelRejectsUnknownName(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
	}

	if _, err := resolvePreferredCustomProviderModel("missing-model", models); err == nil {
		t.Fatal("expected unknown discovered model to fail validation")
	}
}
