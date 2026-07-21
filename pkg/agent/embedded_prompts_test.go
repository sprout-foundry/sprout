package agent

import (
	"fmt"
	"os"
	"regexp"
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
// content (system prompt body, context files, memories). The order matters
// for prompt-prefix cache eligibility: cwd is volatile per-call content
// and must be grouped at the tail so the large static prefix remains
// cacheable across requests.
//
// Note: date/time used to live here too, but was moved to the user message
// (see injectUserMessageTimestamp) because second-resolution timestamps
// invalidated the prefix cache on every turn. This test now asserts the
// cache-eligibility invariant directly: the system prompt bytes are
// byte-identical across two consecutive calls. That is the property we
// actually need for providers to cache the prefix.
func TestSystemPromptCwdOrdering(t *testing.T) {
	first, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Fatalf("expected no error on first call, got: %v", err)
	}
	second, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Fatalf("expected no error on second call, got: %v", err)
	}

	cwdIdx := strings.Index(first, "## Current Working Directory")
	if cwdIdx == -1 {
		t.Fatal("prompt missing '## Current Working Directory'")
	}

	// Cache eligibility: two consecutive calls must produce byte-identical
	// output. If anything per-call (date, time, randomness) leaks in here,
	// Anthropic / OpenAI prompt caching breaks and the user pays for
	// re-processing the prefix on every turn.
	if first != second {
		t.Errorf("system prompt must be byte-identical across consecutive calls for prompt caching to work")
	}

	// The system prompt must mention the timestamp location so the model
	// knows to look for it, but it must NOT contain a real date value —
	// that would be the very leak this design forbids.
	if !strings.Contains(first, "<current-time>") {
		t.Error("system prompt should mention the <current-time> tag so the model knows where to find the current timestamp")
	}
	if strings.Contains(first, "Current date:") {
		t.Error("system prompt must not contain a literal 'Current date:' line — date/time lives in the user message to preserve prompt caching")
	}
	if strings.Contains(first, "Current time:") {
		t.Error("system prompt must not contain a literal 'Current time:' line — date/time lives in the user message to preserve prompt caching")
	}
}

// TestSystemPromptCacheEligibleAcrossProfiles verifies the same cache-eligibility
// invariant for both full and lite profiles. If either profile accidentally
// reintroduces volatile content in its tail, prefix caching fails for users on
// that profile.
func TestSystemPromptCacheEligibleAcrossProfiles(t *testing.T) {
	cases := []struct {
		name    string
		profile configuration.ContextProfile
	}{
		{"full", configuration.ContextProfile{Mode: configuration.ContextModeFull}},
		{"lite", configuration.ContextProfile{Mode: configuration.ContextModeLowContext}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := GetEmbeddedSystemPromptForProfile(tc.profile, "zai", 128000, "/tmp")
			if err != nil {
				t.Fatalf("first call failed: %v", err)
			}
			second, err := GetEmbeddedSystemPromptForProfile(tc.profile, "zai", 128000, "/tmp")
			if err != nil {
				t.Fatalf("second call failed: %v", err)
			}
			if first != second {
				t.Errorf("%s prompt must be byte-identical across calls for cache eligibility", tc.name)
			}
			if strings.Contains(first, "Current date:") {
				t.Errorf("%s prompt leaked a 'Current date:' literal — must live in user message only", tc.name)
			}
		})
	}
}

// TestInjectUserMessageTimestamp verifies the user-message timestamp tag
// format and behavior. The model relies on this tag to reason about timing;
// it's also the only place per-call time appears (system prompt is cached).
func TestInjectUserMessageTimestamp(t *testing.T) {
	t.Run("prepends tag with ISO timestamp", func(t *testing.T) {
		out := injectUserMessageTimestamp("hello")
		if !strings.HasPrefix(out, "<current-time>") {
			t.Errorf("output should start with <current-time> tag, got %q", out[:min(50, len(out))])
		}
		if !strings.Contains(out, "</current-time>") {
			t.Error("output should contain closing </current-time> tag")
		}
		if !strings.HasSuffix(out, "hello") {
			t.Error("user query should remain at the end of the output")
		}
	})

	t.Run("empty input is passed through unchanged", func(t *testing.T) {
		for _, in := range []string{"", " ", "\t\n"} {
			if got := injectUserMessageTimestamp(in); got != in {
				t.Errorf("input %q should pass through unchanged, got %q", in, got)
			}
		}
	})

	t.Run("contains RFC3339 timestamp", func(t *testing.T) {
		out := injectUserMessageTimestamp("test")
		// RFC3339 looks like 2026-07-21T13:42:01Z or 2026-07-21T13:42:01-07:00.
		// Match the date+time+T separator and a timezone marker (Z, +, or -).
		tsRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})`)
		match := tsRe.FindString(out)
		if match == "" {
			t.Errorf("expected RFC3339 timestamp in output, got %q", out)
		}
	})

	t.Run("contains Local parenthetical", func(t *testing.T) {
		out := injectUserMessageTimestamp("test")
		if !strings.Contains(out, "(Local:") {
			t.Error("output should include '(Local: ...)' parenthetical for human readability")
		}
	})
}
