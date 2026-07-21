package agent

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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

	t.Logf("[OK] All providers use consolidated base prompt")
	t.Logf("Base prompt length: %d", len(basePrompt))
}

func TestReadEmbeddedPromptFileWithRepoRelativePath(t *testing.T) {
	content, err := readEmbeddedPromptFile("pkg/agent/prompts/subagent_prompts/web_scraper.md")
	if err != nil {
		t.Fatalf("expected embedded prompt lookup to succeed, got: %v", err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		t.Fatal("expected non-empty embedded prompt content")
	}
}

// TestSystemPromptContainsCwd verifies every system-prompt builder injects a
// "Current Working Directory" section with the real cwd. Fallback to "." is
// allowed but should never appear if os.Getwd succeeds.
func TestSystemPromptContainsCwd(t *testing.T) {
	realCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("test pre-condition: os.Getwd failed: %v", err)
	}

	builders := []struct {
		name string
		fn   func() (string, error)
	}{
		{"GetEmbeddedSystemPrompt", GetEmbeddedSystemPrompt},
		{"GetEmbeddedSystemPromptWithProvider", func() (string, error) {
			return GetEmbeddedSystemPromptWithProvider("zai")
		}},
		{"GetEmbeddedSystemPromptForProfile/full", func() (string, error) {
			return GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{
				Mode: configuration.ContextModeFull,
			}, "zai", 200000, "")
		}},
		{"GetEmbeddedSystemPromptForProfile/lite", func() (string, error) {
			return GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{
				Mode: configuration.ContextModeLowContext,
			}, "zai", 32000, "")
		}},
	}

	for _, tc := range builders {
		t.Run(tc.name, func(t *testing.T) {
			prompt, err := tc.fn()
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if !strings.Contains(prompt, "## Current Working Directory") {
				t.Error("prompt should contain '## Current Working Directory' section header")
			}
			expected := fmt.Sprintf("`%s`", realCwd)
			if !strings.Contains(prompt, expected) {
				t.Errorf("prompt should contain the actual cwd %q", expected)
			}
		})
	}
}

// TestSystemPromptCwdOrdering verifies cwd is positioned AFTER all static
// content (system prompt body, context files, memories) and BEFORE the volatile
// date/time block. The order matters for prompt-prefix cache eligibility:
// volatile per-call content must be grouped at the tail so the large static
// prefix remains cacheable across requests.
func TestSystemPromptCwdOrdering(t *testing.T) {
	prompt, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	cwdIdx := strings.Index(prompt, "## Current Working Directory")
	dateTimeIdx := strings.Index(prompt, "## Current Date and Time")

	if cwdIdx == -1 {
		t.Fatal("prompt missing '## Current Working Directory'")
	}
	if dateTimeIdx == -1 {
		t.Fatal("prompt missing '## Current Date and Time'")
	}
	if cwdIdx >= dateTimeIdx {
		t.Errorf("cwd section (idx %d) must appear BEFORE date/time section (idx %d) — cwd is volatile per-call content and must be grouped with the date/time tail to preserve prompt-prefix cache eligibility", cwdIdx, dateTimeIdx)
	}
}
