package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestNeutralizeSpecialTokens(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no tokens", "plain content", "plain content"},
		{"im_start", "uses <|im_start|> markers", "uses ⟨im_start⟩ markers"},
		{"im_end mid", "a<|im_end|>b", "a⟨im_end⟩b"},
		{"multiple kinds", "<|im_start|> and <|im_end|> and <|endoftext|>", "⟨im_start⟩ and ⟨im_end⟩ and ⟨endoftext⟩"},
		{"not special", "x < 1 | 2 > y", "x < 1 | 2 > y"},
		{"pipe prefix only", "<|unknownthing|>", "<|unknownthing|>"},
	}
	for _, c := range cases {
		if got := NeutralizeSpecialTokens(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestConvertMessagesNeutralizeOptIn(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "read the template with <|im_end|>"},
		{Role: "assistant", Content: "found `<|im_start|>` in file", ToolCalls: []api.ToolCall{{
			ID: "c1", Type: "function",
			Function: api.ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"content":"token = \"<|im_end|>\"","path":"/tmp/x"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "wrote file with <|im_end|> inside"},
	}

	// Opt-in: content neutralized, tool-call arguments untouched.
	on := &GenericProvider{config: &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID:       true,
			NeutralizeSpecialTokens: true,
		},
	}, model: "qwen3.6-27b"}
	got := on.convertMessages(messages, "")
	for _, m := range got {
		if m["role"] == "tool" {
			if c, _ := m["content"].(string); !strings.Contains(c, "⟨im_end⟩") {
				t.Errorf("tool content not neutralized: %q", c)
			}
		}
		if m["role"] == "assistant" {
			if c, _ := m["content"].(string); !strings.Contains(c, "⟨im_start⟩") {
				t.Errorf("assistant content not neutralized: %q", c)
			}
			if tcs, ok := m["tool_calls"].([]map[string]interface{}); ok {
				for _, tc := range tcs {
					fn, _ := tc["function"].(map[string]interface{})
					args, _ := fn["arguments"].(string)
					// json.Marshal HTML-escapes '<' to \u003c, so assert on the
					// decoded value rather than raw substring.
					var parsed map[string]interface{}
					if err := json.Unmarshal([]byte(args), &parsed); err != nil {
						t.Fatalf("tool args not valid JSON: %v", err)
					}
					fileContent, _ := parsed["content"].(string)
					if strings.Contains(fileContent, "⟨") {
						t.Errorf("tool-call arguments must never be neutralized, got %q", fileContent)
					}
					if !strings.Contains(fileContent, "<|im_end|>") {
						t.Errorf("tool-call arguments must keep raw token bytes, got %q", fileContent)
					}
				}
			}
		}
	}

	// Default off: bytes pass through unchanged.
	off := &GenericProvider{config: &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID: true,
		},
	}, model: "qwen3.6-27b"}
	got2 := off.convertMessages(messages, "")
	for _, m := range got2 {
		if m["role"] == "tool" {
			if c, _ := m["content"].(string); !strings.Contains(c, "<|im_end|>") {
				t.Errorf("default must not neutralize, got %q", c)
			}
		}
	}
}

func TestNeutralizeSkipsSystemMessages(t *testing.T) {
	p := &GenericProvider{config: &ProviderConfig{
		Name:       "ai-worker",
		Conversion: MessageConversion{NeutralizeSpecialTokens: true},
	}, model: "m"}
	got := p.convertMessages([]api.Message{
		{Role: "system", Content: "system may legitimately mention <|im_end|> in instructions"},
	}, "")
	c, _ := got[0]["content"].(string)
	if !strings.Contains(c, "<|im_end|>") {
		t.Errorf("system content should be left as-is, got %q", c)
	}
}
