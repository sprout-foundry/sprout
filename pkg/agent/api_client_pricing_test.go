package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestStripImagesFromMessages(t *testing.T) {
	t.Parallel()

	t.Run("empty slice", func(t *testing.T) {
		t.Parallel()
		result, hadImages := stripImagesFromMessages(nil)
		if hadImages {
			t.Error("expected no images")
		}
		if len(result) != 0 {
			t.Errorf("expected empty, got %d", len(result))
		}
	})

	t.Run("no images", func(t *testing.T) {
		t.Parallel()
		msgs := []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		}
		result, hadImages := stripImagesFromMessages(msgs)
		if hadImages {
			t.Error("expected no images")
		}
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
	})

	t.Run("with images strips them", func(t *testing.T) {
		t.Parallel()
		msgs := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{{Base64: "abc", Type: "image/png"}}},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "and this", Images: []api.ImageData{{Base64: "def", Type: "image/png"}}},
		}
		result, hadImages := stripImagesFromMessages(msgs)
		if !hadImages {
			t.Error("expected images to be detected")
		}
		if len(result) != 3 {
			t.Errorf("expected 3, got %d", len(result))
		}
		for _, m := range result {
			if len(m.Images) != 0 {
				t.Errorf("expected no images in message with role %s", m.Role)
			}
		}
		// Verify Content and Role are preserved
		if result[0].Content != "look" || result[0].Role != "user" {
			t.Errorf("content/role not preserved at index 0: Content=%q Role=%q", result[0].Content, result[0].Role)
		}
		if result[1].Content != "ok" || result[1].Role != "assistant" {
			t.Errorf("content/role not preserved at index 1: Content=%q Role=%q", result[1].Content, result[1].Role)
		}
	})

	t.Run("does not mutate original", func(t *testing.T) {
		t.Parallel()
		msgs := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{{Base64: "abc", Type: "image/png"}}},
		}
		_, _ = stripImagesFromMessages(msgs)
		if len(msgs[0].Images) == 0 {
			t.Error("original was mutated")
		}
	})
}

func TestEstimateCompletionTokensFromResponse(t *testing.T) {
	t.Parallel()

	t.Run("nil response", func(t *testing.T) {
		t.Parallel()
		if got := estimateCompletionTokensFromResponse(nil); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})

	t.Run("empty choices", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{}
		if got := estimateCompletionTokensFromResponse(resp); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})

	t.Run("single choice", func(t *testing.T) {
		t.Parallel()
		content := "Hello, this is a test response with some content"
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{},
			},
		}
		resp.Choices[0].Message.Content = content
		got := estimateCompletionTokensFromResponse(resp)
		if got <= 0 {
			t.Errorf("expected positive tokens, got %d", got)
		}
		// Should be roughly len/4 (EstimateTokens uses ~4 chars per token)
		approxExpected := len(content) / 4
		if got < approxExpected/2 || got > approxExpected*2 {
			t.Errorf("got %d, expected roughly %d", got, approxExpected)
		}
	})

	t.Run("multiple choices", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{},
				{},
			},
		}
		resp.Choices[0].Message.Content = "short"
		resp.Choices[1].Message.Content = "another response here"
		got := estimateCompletionTokensFromResponse(resp)
		if got <= 0 {
			t.Errorf("expected positive tokens, got %d", got)
		}
	})

	t.Run("empty content in choice", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{},
			},
		}
		// Content field is zero-value (empty string)
		if got := estimateCompletionTokensFromResponse(resp); got != 0 {
			t.Errorf("expected 0 for empty content, got %d", got)
		}
	})

	t.Run("with reasoning content", func(t *testing.T) {
		t.Parallel()
		resp := &api.ChatResponse{
			Choices: []api.Choice{
				{},
			},
		}
		resp.Choices[0].Message.Content = "output"
		resp.Choices[0].Message.ReasoningContent = "thinking about the answer carefully"
		got := estimateCompletionTokensFromResponse(resp)
		if got <= 0 {
			t.Errorf("expected positive tokens, got %d", got)
		}
		// Should be more than just "output" alone
		outputOnly := EstimateTokens("output")
		if got <= outputOnly {
			t.Errorf("expected more tokens with reasoning, got %d vs %d", got, outputOnly)
		}
	})
}

func TestGetModelPricing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		model           string
		provider        string
		wantInputExact  float64 // 0 means only check positivity
		wantOutputExact float64
	}{
		// Default fallback — verify actual defaults
		{name: "unknown provider and model returns defaults", model: "some-model", provider: "unknown-provider", wantInputExact: 1.0, wantOutputExact: 2.0},

		// DeepInfra
		{name: "deepinfra deepseek-v3", model: "deepseek-v3", provider: "deepinfra", wantInputExact: 0.27, wantOutputExact: 1.10},
		{name: "deepinfra deepseek-v2", model: "deepseek-v2-lite", provider: "deepinfra", wantInputExact: 0.27, wantOutputExact: 1.10},
		{name: "deepinfra deepseek-r1", model: "deepseek-r1", provider: "deepinfra", wantInputExact: 0.55, wantOutputExact: 2.19},
		{name: "deepinfra llama-3.3", model: "llama-3.3-70b", provider: "deepinfra", wantInputExact: 0.88, wantOutputExact: 0.88},
		{name: "deepinfra llama-3.1-70b", model: "llama-3.1-70b", provider: "deepinfra", wantInputExact: 0.88, wantOutputExact: 0.88},
		{name: "deepinfra llama-3.1-405b", model: "llama-3.1-405b", provider: "deepinfra", wantInputExact: 5.00, wantOutputExact: 5.00},
		{name: "deepinfra qwen-2.5", model: "qwen-2.5-72b", provider: "deepinfra", wantInputExact: 0.30, wantOutputExact: 0.60},
		{name: "deepinfra mistral", model: "mistral-7b", provider: "deepinfra", wantInputExact: 0.24, wantOutputExact: 0.24},

		// OpenRouter
		{name: "openrouter deepseek-chat", model: "deepseek-chat", provider: "openrouter", wantInputExact: 0.55, wantOutputExact: 2.19},
		{name: "openrouter deepseek-r1", model: "deepseek-r1", provider: "openrouter", wantInputExact: 0.55, wantOutputExact: 2.19},
		{name: "openrouter gpt-4o", model: "gpt-4o", provider: "openrouter", wantInputExact: 2.50, wantOutputExact: 10.00},
		{name: "openrouter gpt-4-turbo", model: "gpt-4-turbo", provider: "openrouter", wantInputExact: 10.00, wantOutputExact: 30.00},
		{name: "openrouter gpt-4 base", model: "gpt-4", provider: "openrouter", wantInputExact: 30.00, wantOutputExact: 60.00},
		{name: "openrouter claude-3.5-sonnet", model: "claude-3.5-sonnet", provider: "openrouter", wantInputExact: 3.00, wantOutputExact: 15.00},
		{name: "openrouter claude-3-opus", model: "claude-3-opus", provider: "openrouter", wantInputExact: 15.00, wantOutputExact: 75.00},
		{name: "openrouter claude-3-haiku", model: "claude-3-haiku", provider: "openrouter", wantInputExact: 0.25, wantOutputExact: 1.25},
		{name: "openrouter llama-3.1-405b", model: "llama-3.1-405b", provider: "openrouter", wantInputExact: 5.00, wantOutputExact: 5.00},
		{name: "openrouter llama-3.1-70b", model: "llama-3.1-70b", provider: "openrouter", wantInputExact: 0.88, wantOutputExact: 0.88},
		{name: "openrouter llama-3.1-8b", model: "llama-3.1-8b", provider: "openrouter", wantInputExact: 0.18, wantOutputExact: 0.18},

		// OpenAI
		{name: "openai gpt-4o", model: "gpt-4o", provider: "openai", wantInputExact: 2.50, wantOutputExact: 10.00},
		{name: "openai gpt-4-turbo", model: "gpt-4-turbo", provider: "openai", wantInputExact: 10.00, wantOutputExact: 30.00},
		{name: "openai gpt-4 base", model: "gpt-4", provider: "openai", wantInputExact: 30.00, wantOutputExact: 60.00},
		{name: "openai gpt-3.5-turbo", model: "gpt-3.5-turbo", provider: "openai", wantInputExact: 0.50, wantOutputExact: 1.50},

		// DeepInfra provider-specific qwen3-coder overlap
		{name: "deepinfra qwen3-coder", model: "qwen3-coder-30b", provider: "deepinfra", wantInputExact: 0.30, wantOutputExact: 0.60},

		// OpenRouter claude-3-sonnet (without .5)
		{name: "openrouter claude-3-sonnet", model: "claude-3-sonnet", provider: "openrouter", wantInputExact: 3.00, wantOutputExact: 15.00},

		// Generic model-based
		{name: "generic gpt-oss", model: "gpt-oss-1", provider: "other", wantInputExact: 0.30, wantOutputExact: 0.60},
		{name: "generic qwen3-coder", model: "qwen3-coder-30b", provider: "other", wantInputExact: 0.30, wantOutputExact: 0.60},
		{name: "generic qwen-coder", model: "qwen-coder-7b", provider: "other", wantInputExact: 0.30, wantOutputExact: 0.60},
		{name: "generic llama", model: "llama-2-70b", provider: "other", wantInputExact: 0.36, wantOutputExact: 0.36},
		{name: "generic deepseek", model: "deepseek-chat", provider: "other", wantInputExact: 0.55, wantOutputExact: 2.19},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in, out := getModelPricing(tt.model, tt.provider)
			if in <= 0 || out <= 0 {
				t.Errorf("getModelPricing(%q, %q) = (%v, %v), expected positive values", tt.model, tt.provider, in, out)
			}
			if tt.wantInputExact > 0 && in != tt.wantInputExact {
				t.Errorf("input cost = %v, want %v", in, tt.wantInputExact)
			}
			if tt.wantOutputExact > 0 && out != tt.wantOutputExact {
				t.Errorf("output cost = %v, want %v", out, tt.wantOutputExact)
			}
		})
	}
}

func TestGetModelPricing_CaseInsensitive(t *testing.T) {
	t.Parallel()
	in1, out1 := getModelPricing("GPT-4o", "OpenAI")
	in2, out2 := getModelPricing("gpt-4o", "openai")
	if in1 != in2 || out1 != out2 {
		t.Errorf("case insensitive mismatch: (%v,%v) vs (%v,%v)", in1, out1, in2, out2)
	}
}
