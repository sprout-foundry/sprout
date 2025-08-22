package llm

import (
	"os"
	"path/filepath"
	"testing"
)

type mockLLMClient struct{}

func (m *mockLLMClient) Generate(prompt string) (string, error) {
	return "mock response", nil
}

func TestMain(m *testing.M) {
	RegisterProvider("mock", func() (LLMClient, error) { return &mockLLMClient{}, nil })
	os.Exit(m.Run())
}

func TestGetModelPricing_DefaultsAndOverrides(t *testing.T) {
	// Work in temp dir to isolate .ledit path
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Reinitialize pricing table in this temp context
	pricingInitDone = false
	if err := InitPricingTable(); err != nil {
		t.Fatalf("InitPricingTable: %v", err)
	}

	// Default heuristic
	p := GetModelPricing("deepinfra:deepseek-ai/DeepSeek-V3")
	if p.InputCostPer1K == 0 && p.OutputCostPer1K == 0 {
		t.Fatalf("unexpected zero pricing for deepseek heuristic")
	}

	// Override and persist
	custom := ModelPricing{InputCostPer1K: 0.123, OutputCostPer1K: 0.456}
	if err := UpdatePricing("my-model", custom); err != nil {
		t.Fatalf("UpdatePricing: %v", err)
	}
	// Ensure file exists
	if _, err := os.Stat(filepath.Join(".ledit", "model_pricing.json")); err != nil {
		t.Fatalf("expected pricing file: %v", err)
	}
	// Lookup override (case-insensitive)
	got := GetModelPricing(" My-Model ")
	if got.InputCostPer1K != custom.InputCostPer1K || got.OutputCostPer1K != custom.OutputCostPer1K {
		t.Fatalf("override not applied: %+v", got)
	}
}

func TestNewLLMClientWithMockProvider(t *testing.T) {
	client, err := NewLLMClient("mock")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_, err = client.Generate("test")
	if err != nil {
		t.Fatalf("expected no error from mock client, got %v", err)
	}
}
