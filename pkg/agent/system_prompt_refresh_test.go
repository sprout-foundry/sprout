package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestRefreshSystemPrompt_FlagOff verifies that when the config flag
// RefreshSystemPromptOnModelChange is false (the default), refreshSystemPrompt
// is a strict no-op. This preserves the legacy "prompt baked at agent
// creation" contract that existing sessions and tests depend on.
//
// The test creates an agent through NewAgentWithClient with the test mock
// (which produces the full orchestrator prompt via the 128K-context default),
// then calls refreshSystemPrompt and asserts the prompt is byte-identical.
// If the flag was off but the prompt still changed, that would mean the
// re-derivation path fired even when it shouldn't.
func TestRefreshSystemPrompt_FlagOff(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// Leave RefreshSystemPromptOnModelChange at its zero value (false).
	// Don't mutate — this is the point: the default must be honored
	// without any user opt-in.
	if mgr.GetConfig().GetRefreshSystemPromptOnModelChange() {
		t.Fatal("test precondition: default flag value should be false")
	}

	agent, err := NewAgentWithClient(
		NewMockLLMProviderWithLimit(128_000),
		api.TestClientType,
		mgr,
	)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	defer agent.Shutdown()

	originalPrompt := agent.GetSystemPrompt()
	originalBasePrompt := agent.baseSystemPrompt

	// Direct call (not via setClient). refreshSystemPrompt should
	// observe the flag-off state and return without touching anything.
	agent.refreshSystemPrompt()

	if agent.GetSystemPrompt() != originalPrompt {
		t.Errorf("flag-off should leave systemPrompt unchanged\n got: first 120 chars %q\nwant: first 120 chars %q",
			first120(agent.GetSystemPrompt()), first120(originalPrompt))
	}
	if agent.baseSystemPrompt != originalBasePrompt {
		t.Errorf("flag-off should leave baseSystemPrompt unchanged")
	}
}

// TestRefreshSystemPrompt_FlagOn_DifferentProvider verifies that when the
// config flag is true, refreshSystemPrompt actually re-derives the system
// prompt after the active client changes.
//
// Spec note: the original test plan asked for provider-specific keywords
// ("openai" vs "anthropic") but the embedded prompts do not currently
// differ by provider — see TestGetEmbeddedSystemPromptWithProvider in
// embedded_prompts_test.go, which explicitly asserts all providers get
// the same base prompt. The provider argument to
// GetEmbeddedSystemPromptForProfile is currently unused; the real
// differentiator is the resolved ContextProfile (full vs lite). The test
// below exercises the *same* re-derivation path through a context-window
// change (128K → 32K), which forces LCM auto-detection and swaps the
// embedded prompt from full to lite. This is the empirical adjustment the
// spec explicitly authorized: "Adjust the test to use whatever the embedded
// prompts actually contain — be empirical."
//
// The test asserts both directions of the transition so a regression that
// silently swaps the prompt content (or fails to swap it) is caught.
func TestRefreshSystemPrompt_FlagOn_DifferentProvider(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.RefreshSystemPromptOnModelChange = true
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	// Start with a 128K-context client → full-context profile → the
	// orchestrator prompt (~6.6K tokens, contains "Orchestrator" and
	// "Persona Selection Guide" markers).
	agent, err := NewAgentWithClient(
		NewMockLLMProviderWithLimit(128_000),
		api.TestClientType,
		mgr,
	)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	defer agent.Shutdown()

	initialPrompt := agent.GetSystemPrompt()
	if !strings.Contains(initialPrompt, "Orchestrator") {
		t.Fatalf("initial 128K prompt should be the full orchestrator prompt (missing 'Orchestrator' marker); got first 200 chars: %q",
			first200(initialPrompt))
	}
	if strings.Contains(initialPrompt, "Low-Context Mode") {
		t.Fatalf("initial 128K prompt should NOT contain 'Low-Context Mode' (full prompt leaked); got first 200 chars: %q",
			first200(initialPrompt))
	}

	// Swap to a 32K-context client. The 32K window is below
	// subagentContextThreshold (64K), so ResolveContextProfile will
	// auto-detect LCM. setClient triggers refreshSystemPrompt through
	// the new wiring in agent_accessors.go; the assertion below
	// validates that the refresh actually fired and produced the
	// lite prompt.
	agent.setClient(NewMockLLMProviderWithLimit(32_000), api.TestClientType)

	refreshedPrompt := agent.GetSystemPrompt()
	if refreshedPrompt == initialPrompt {
		t.Fatal("setClient should have re-derived the system prompt when flag is on, but prompt is byte-identical to the initial 128K prompt")
	}
	if !strings.Contains(refreshedPrompt, "Low-Context Mode") {
		t.Errorf("32K should produce the lite prompt (missing 'Low-Context Mode' marker); got first 200 chars: %q",
			first200(refreshedPrompt))
	}
	if strings.Contains(refreshedPrompt, "Persona Selection Guide") {
		t.Errorf("32K should NOT contain 'Persona Selection Guide' (lite prompt leaked full-prompt content); got first 200 chars: %q",
			first200(refreshedPrompt))
	}

	// baseSystemPrompt must track systemPrompt — SetBaseSystemPrompt's
	// contract is that clearing persona restores baseSystemPrompt. If
	// refreshSystemPrompt only updated systemPrompt, a later persona
	// reset would reintroduce the stale 128K-era prompt.
	if agent.baseSystemPrompt != refreshedPrompt {
		t.Errorf("baseSystemPrompt must match refreshed systemPrompt\n baseSystemPrompt first 120 chars: %q\n systemPrompt first 120 chars: %q",
			first120(agent.baseSystemPrompt), first120(refreshedPrompt))
	}

	// Round-trip back to 128K and assert the full prompt returns. This
	// catches a regression where refreshSystemPrompt only updates on the
	// first call (e.g., it shorts out after a successful refresh).
	agent.setClient(NewMockLLMProviderWithLimit(128_000), api.TestClientType)

	roundTripPrompt := agent.GetSystemPrompt()
	if !strings.Contains(roundTripPrompt, "Orchestrator") {
		t.Errorf("back-to-128K should restore the full orchestrator prompt (missing 'Orchestrator' marker); got first 200 chars: %q",
			first200(roundTripPrompt))
	}
	if strings.Contains(roundTripPrompt, "Low-Context Mode") {
		t.Errorf("back-to-128K should NOT contain 'Low-Context Mode'; got first 200 chars: %q",
			first200(roundTripPrompt))
	}
}

// TestRefreshSystemPrompt_NoOpWhenConfigMissing verifies that bare Agent
// structs (used widely in test fixtures throughout pkg/agent) don't panic
// when refreshSystemPrompt is called. The method must short-circuit when
// configManager is nil — the system prompt is already correct from the
// agent creation path that produced the struct, and there's nothing to
// re-derive without a config source.
func TestRefreshSystemPrompt_NoOpWhenConfigMissing(t *testing.T) {
	a := &Agent{}
	// Seed an arbitrary "initial" prompt so we can prove it wasn't
	// touched. Bare Agent{} has systemPrompt="" by default; using a
	// known sentinel makes the assertion unambiguous.
	const sentinel = "sentinel-from-test-setup"
	a.systemPrompt = sentinel
	a.baseSystemPrompt = sentinel

	// Must not panic.
	a.refreshSystemPrompt()

	if a.systemPrompt != sentinel {
		t.Errorf("bare Agent.systemPrompt was modified by refreshSystemPrompt\n got: %q\nwant: %q",
			a.systemPrompt, sentinel)
	}
	if a.baseSystemPrompt != sentinel {
		t.Errorf("bare Agent.baseSystemPrompt was modified by refreshSystemPrompt\n got: %q\nwant: %q",
			a.baseSystemPrompt, sentinel)
	}
}

// first120 returns the first 120 characters of s (or all of it if shorter),
// suitable for stable error-message prefixes without dumping full prompts.
func first120(s string) string {
	return firstN(s, 120)
}

// first200 returns the first 200 characters of s (or all of it if shorter).
func first200(s string) string {
	return firstN(s, 200)
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
