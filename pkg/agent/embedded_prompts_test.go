package agent

import (
	"strings"
	"testing"
)

func TestGetEmbeddedSystemPrompt(t *testing.T) {
	prompt, err := GetEmbeddedSystemPrompt()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if prompt == "" {
		t.Error("Expected non-empty system prompt")
	}

	if !strings.Contains(prompt, "You are") {
		t.Error("System prompt should contain agent description")
	}
}

func TestGetEmbeddedSystemPromptWithProvider(t *testing.T) {
	basePrompt, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// All providers should return the same base prompt (GLM-4.6 constraints removed)
	providers := []string{"zai", "openai", "deepinfra"}

	for _, provider := range providers {
		providerPrompt, err := GetEmbeddedSystemPromptWithProvider(provider)
		if err != nil {
			t.Errorf("Expected no error for %s, got: %v", provider, err)
		}
		if len(providerPrompt) != len(basePrompt) {
			t.Errorf("Provider %s should get same base prompt", provider)
		}
	}

	// Verify base prompt has consolidated efficiency guidelines
	if !strings.Contains(basePrompt, "Be concise and direct") {
		t.Error("Base prompt should contain consolidated conciseness instruction")
	}

	if !strings.Contains(basePrompt, "Limit tool usage") {
		t.Error("Base prompt should contain tool usage limits")
	}
}

func TestConsolidatedEfficiencyGuidelines(t *testing.T) {
	// Test that efficiency guidelines are integrated throughout the base prompt
	basePrompt, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Check that key efficiency concepts are present
	expectedIntegrations := []string{
		"Be concise and direct",    // Core Principles
		"Focus on results",         // Core Principles
		"Make decisive choices",    // Tool Usage Guidelines
		"straightforward solution", // Implementation Process
	}

	for _, integration := range expectedIntegrations {
		if !strings.Contains(basePrompt, integration) {
			t.Errorf("Expected to find integrated efficiency instruction: %s", integration)
		}
	}

	// Verify the redundant section was removed
	if strings.Contains(basePrompt, "Efficiency and Communication Guidelines") {
		t.Error("Redundant efficiency section should have been removed")
	}

	// Verify all providers get the same base prompt
	providers := []string{"openai", "deepinfra", "ollama", "zai"}
	for _, provider := range providers {
		providerPrompt, err := GetEmbeddedSystemPromptWithProvider(provider)
		if err != nil {
			t.Errorf("Expected no error for %s, got: %v", provider, err)
		}
		if len(providerPrompt) != len(basePrompt) {
			t.Errorf("Provider %s should get same consolidated base prompt", provider)
		}
	}

	t.Logf("✅ All providers use consolidated base prompt")
	t.Logf("Base prompt length: %d", len(basePrompt))
}
