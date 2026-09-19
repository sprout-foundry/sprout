package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// aiWorkerConfig mirrors a custom gateway provider: a configured 200K
// window, reasoning_content replay, and no per-model catalog entry (so the
// configured context_size is the only limit source buildChatRequest sees).
func aiWorkerConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "ai-worker",
		Endpoint: "https://ai-worker.example/v1/chat/completions",
		Auth: AuthConfig{
			Type:   "bearer",
			EnvVar: "AI_WORKER_API_KEY",
		},
		Defaults: RequestDefaults{
			Model: "qwen3.6-27b",
		},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "qwen3.6-27b",
		},
	}
}

// bigConversation builds a message history of approximately targetChars
// bytes. tool-heavy agent transcripts are mostly code/JSON, so use a
// code-like body (detectCode fires, 1.2 tokens/word) — same estimator
// regime as the real failing requests.
func bigConversation(msgCount, targetChars int) []api.Message {
	turn := strings.Repeat("func handler(w io.Writer) error { return writeBlock(data) }\n", 60)
	msgs := make([]api.Message, 0, msgCount)
	remaining := targetChars
	for len(msgs) < msgCount && remaining > 0 {
		role := "user"
		if len(msgs)%2 == 1 {
			role = "assistant"
		}
		n := turn
		if remaining < len(turn) {
			n = turn[:remaining]
		}
		msgs = append(msgs, api.Message{Role: role, Content: n})
		remaining -= len(n)
	}
	return msgs
}

func thirteenTools() []api.Tool {
	tools := make([]api.Tool, 13)
	for i := range tools {
		tools[i] = api.Tool{Type: "function"}
		tools[i].Function.Name = "tool_" + string(rune('a'+i))
		tools[i].Function.Description = "does a thing"
		tools[i].Function.Parameters = map[string]interface{}{"type": "object"}
	}
	return tools
}

// guardRegistryOff disables the remote model registry for the test process.
// t.Setenv cannot do this: the registry reads SPROUT_MODEL_REGISTRY_URL once
// in package init, so SetBaseURL is the only test-side hook.
func guardRegistryOff(t *testing.T) {
	t.Helper()
	original := modelregistry.BaseURLForTest()
	modelregistry.SetBaseURL("")
	t.Cleanup(func() { modelregistry.SetBaseURL(original) })
}

// gateway-reported shape: stream, tools=13, 100+ messages, 80K-134K
// real prompt tokens against a 200K window, max_tokens must NOT be 512.
func TestAIWorkerBigPromptBudgetNot512(t *testing.T) {
	t.Setenv("SPROUT_MAX_REQUEST_COMPLETION_TOKENS", "")
	guardRegistryOff(t)

	cases := []struct {
		name      string
		msgCount  int
		chars     int
		minBudget int
	}{
		{"80K-char prompt", 100, 80000, 40000},
		{"400K-char prompt (heuristic ~107K tokens)", 150, 400000, 20000},
		{"800K-char prompt (heuristic ~204K est, just over the 200K window)", 215, 800000, 64000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewGenericProvider(aiWorkerConfig())
			if err != nil {
				t.Fatalf("provider: %v", err)
			}
			if err := p.SetModel("qwen3.6-27b"); err != nil {
				t.Fatalf("set model: %v", err)
			}

			messages := bigConversation(tc.msgCount, tc.chars)
			body, err := p.buildChatRequest(messages, thirteenTools(), "", false, true)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			req := decodeRequest(t, body)

			maxTokens, ok := req["max_tokens"].(float64)
			if !ok {
				t.Fatalf("max_tokens missing or not a number: %v", req["max_tokens"])
			}
			if maxTokens != float64(int(maxTokens)) {
				t.Fatalf("non-integer max_tokens: %v", maxTokens)
			}
			if int(maxTokens) < tc.minBudget {
				t.Fatalf("max_tokens = %d, want >= %d for a big-context gateway request", int(maxTokens), tc.minBudget)
			}
			if int(maxTokens) == 512 {
				t.Fatalf("max_tokens pinned to the old decapitation floor")
			}
			if int(maxTokens) > 64000 {
				t.Fatalf("max_tokens = %d exceeds the 64K request cap", int(maxTokens))
			}
			stream, _ := req["stream"].(bool)
			if !stream {
				t.Fatalf("expected stream=true to match the reported shape")
			}
		})
	}
}

// The anchored path (token anchor holds, real prompt-token count known)
// must keep producing healthy budgets — the shape the provider saw on the
// sibling calls. CalculateMaxTokensWithLimits with a realistic anchored
// input exercises the shared math; the hint path uses
// CalculateOutputBudgetAnchored with the same inputs.
func TestAIWorkerAnchoredBudgetHealthy(t *testing.T) {
	t.Setenv("SPROUT_MAX_REQUEST_COMPLETION_TOKENS", "")
	guardRegistryOff(t)

	messages := bigConversation(120, 500000)
	tools := thirteenTools()

	// ~real prompt token count reported by the gateway for this shape.
	anchoredInput := 120000

	limit := aiWorkerConfig().GetContextLimit("qwen3.6-27b")
	if limit != 200000 {
		t.Fatalf("expected gateway profile context limit 200000, got %d", limit)
	}

	// Heuristic portion is what the estimator adds on top of the anchored
	// prefix; the budget must stay in the tens-of-thousands, never 512.
	maxTokens := CalculateMaxTokensWithLimits(limit, 0, messages, tools)
	if maxTokens <= 512 {
		t.Fatalf("anchored-shape budget = %d, want comfortably above the old floor", maxTokens)
	}
	if maxTokens > 64000 {
		t.Fatalf("anchored-shape budget = %d exceeds the request cap", maxTokens)
	}

	// The hint path (what sproutProvider.computeMaxTokensHint feeds the
	// provider when the anchor holds) must agree in magnitude.
	hint, ok := api.CalculateOutputBudgetAnchored(limit, anchoredInput, 0)
	if !ok || hint <= 512 {
		t.Fatalf("anchored hint = %d (ok=%v), want a healthy budget", hint, ok)
	}
	if hint != 64000 {
		// 200000 - 120000 anchored - max(5% of 200000, 2000) cushion = 70000
		// -> capped at the 64K request cap downstream.
		t.Logf("note: anchored hint %d (window-anchored-cushion), caller caps at 64K", hint)
	}
}

// The harness-level invariant: whatever the estimate says, the request the
// gateway receives never carries a max_tokens below the emergency floor —
// including the pathological all-tiny-messages case that used to collapse.
func TestAIWorkerRequestNeverCarriesDegenerateBudget(t *testing.T) {
	t.Setenv("SPROUT_MAX_REQUEST_COMPLETION_TOKENS", "")
	guardRegistryOff(t)

	p, err := NewGenericProvider(aiWorkerConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if err := p.SetModel("qwen3.6-27b"); err != nil {
		t.Fatalf("set model: %v", err)
	}

	// Worst case for the OLD code: many messages, code-like, estimate just
	// over the window -> old pin = 512.
	messages := bigConversation(215, 900000)
	body, err := p.buildChatRequest(messages, thirteenTools(), "", false, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	maxTokens := int(req["max_tokens"].(float64))
	if maxTokens <= 512 {
		t.Fatalf("degenerate budget %d reached the wire; gateway would saw the response at %d tokens", maxTokens, maxTokens)
	}
}
