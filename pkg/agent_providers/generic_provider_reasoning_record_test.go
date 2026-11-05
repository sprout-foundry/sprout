package providers

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestReasoningTokensForResponse pins rel.1's "0 reasoning tokens is
// visible": the per-call audit event carries an estimated reasoning-token
// count from the captured reasoning_content, so a model that is supposed to
// be reasoning can be told from one that is not.
func TestReasoningTokensForResponse(t *testing.T) {
	if got := reasoningTokensForResponse(nil); got != 0 {
		t.Errorf("nil response = %d, want 0", got)
	}
	if got := reasoningTokensForResponse(&api.ChatResponse{}); got != 0 {
		t.Errorf("empty response = %d, want 0", got)
	}
	resp := &api.ChatResponse{
		Choices: []api.Choice{{
			Message: api.Message{ReasoningContent: "step one then step two then the answer"},
		}},
	}
	if got := reasoningTokensForResponse(resp); got <= 0 {
		t.Errorf("reasoning_content present = %d, want > 0", got)
	}
}

// TestApplyReasoningEffortOpenRouterDefaultDepth pins the rel.1 default
// policy on the unified-object path: a reasoning-capable model with no
// configured effort still gets the unified reasoning object at a default
// depth, so reasoning is on by default.
func TestApplyReasoningEffortOpenRouterDefaultDepth(t *testing.T) {
	cfg := &ProviderConfig{
		Name:     "openrouter",
		Endpoint: "https://openrouter.ai/api/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		Conversion: MessageConversion{
			UnifiedReasoningParam: true,
		},
		Models: ModelConfig{DefaultContextLimit: 131072},
	}
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	request := map[string]interface{}{}
	p.applyReasoningEffort("anthropic/claude-sonnet-4.6", "", request)
	reasoning, ok := request["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a reasoning object for a reasoning-capable model, got %#v", request["reasoning"])
	}
	effort, _ := reasoning["effort"].(string)
	if effort == "" {
		t.Error("expected a default reasoning effort, got empty")
	}
}
