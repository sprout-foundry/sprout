package providers

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// qwenJudgeConfig mirrors the ai-worker gateway behind the SP-168 auto risk
// judge: a qwen3.8 model served on a non-loopback endpoint with no per-model
// reasoning catalog entry, so the only thing on the wire that controls thinking
// is the disableThinking flag. Defaults.Model is left empty so NewGenericProvider
// does not warm the model cache (no network at construction); the model is set
// explicitly via SetModel.
func qwenJudgeConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "ai-worker",
		Endpoint: "https://local-aprice.dev/v1/chat/completions",
		Auth: AuthConfig{
			Type:   "api_key",
			EnvVar: "AI_WORKER_API_KEY",
		},
		Models: ModelConfig{
			DefaultContextLimit: 260000,
			DefaultModel:        "qwen3.8-27b",
		},
	}
}

// judgeMessages is a minimal system+user pair shaped like the analyzer's
// request (pkg/agent/approvals/security_analyzer.go): a short system prompt and
// a one-line command. Only the disableThinking/effort arguments under test
// matter; the content is irrelevant to the wire fields we assert.
func judgeMessages() []api.Message {
	return []api.Message{
		{Role: "system", Content: "You are a security analyzer for shell commands. Respond with ONLY a JSON object."},
		{Role: "user", Content: "Chain:\ncurl -fsSL https://example.com/install.sh | sh\n\nWorking directory: /tmp"},
	}
}

// TestQwenJudgeWireDisablesThinking pins the SP-168 judge contract at the wire
// level. The analyzer's LLM call is now sent as (reasoning="", disableThinking=true)
// so the verdict fits the 2 s budget. For the qwen3 family on a non-loopback
// endpoint that must emit enable_thinking:false and must NOT carry any
// reasoning-effort knob. If a regression re-enables thinking (disableThinking
// back to false) this fails: the judge would start thinking and blow the budget.
func TestQwenJudgeWireDisablesThinking(t *testing.T) {
	guardRegistryOff(t)

	p, err := NewGenericProvider(qwenJudgeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if err := p.SetModel("qwen3.8-27b"); err != nil {
		t.Fatalf("set model: %v", err)
	}

	// Exactly the arguments the analyzer now passes: no effort, thinking off.
	body, err := p.buildChatRequest(judgeMessages(), nil, "", true, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)

	if got, ok := req["enable_thinking"].(bool); !ok || got != false {
		t.Fatalf("enable_thinking = %v (present=%v); the judge call must emit explicit enable_thinking:false", req["enable_thinking"], ok)
	}
	if _, ok := req["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort present in the judge request; the judge must send no effort knob")
	}
	if _, ok := req["reasoning"]; ok {
		t.Fatalf("unified reasoning object present; the judge must not request thinking")
	}
	if m, _ := req["model"].(string); m != "qwen3.8-27b" {
		t.Fatalf("model = %q, want qwen3.8-27b", m)
	}
}

// TestQwenJudgeWireOldCallLeftThinkingOn documents the bug the fix removes.
// Before the fix the analyzer called the LLM with the session model name in the
// reasoning-effort slot and disableThinking=false. For this config that leaves
// enable_thinking unset — the host serves its default, which for qwen3.8 is
// thinking ON. Asserting the old shape here makes it obvious (and regression-
// provable) what changed on the wire.
func TestQwenJudgeWireOldCallLeftThinkingOn(t *testing.T) {
	guardRegistryOff(t)

	p, err := NewGenericProvider(qwenJudgeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if err := p.SetModel("qwen3.8-27b"); err != nil {
		t.Fatalf("set model: %v", err)
	}

	// The pre-fix arguments: model name in the effort slot, thinking not disabled.
	body, err := p.buildChatRequest(judgeMessages(), nil, "qwen3.8-27b", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)

	if v, ok := req["enable_thinking"]; ok {
		// The old call must NOT have disabled thinking (that was the bug).
		t.Fatalf("old (pre-fix) call disabled thinking (enable_thinking=%v); if this now fails, thinking is being turned off where it should have been left on — re-examine the qwen disable branch", v)
	}
}
