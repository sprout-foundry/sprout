package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestSelectDefaultModel(t *testing.T) {
	a := &Agent{}
	tests := []struct {
		name     string
		provider api.ClientType
		models   []api.ModelInfo
		want     string
	}{
		{"empty", api.DeepInfraClientType, nil, ""},
		{"deepinfra ordered compound pattern", api.DeepInfraClientType, []api.ModelInfo{{ID: "other"}, {ID: "DeepSeek-Chat"}, {ID: "DeepSeek-Coder-Instruct"}}, "DeepSeek-Coder-Instruct"},
		{"deepinfra fallback pattern", api.DeepInfraClientType, []api.ModelInfo{{ID: "other"}, {ID: "DeepSeek-Chat"}}, "DeepSeek-Chat"},
		{"openrouter free", api.OpenRouterClientType, []api.ModelInfo{{ID: "paid"}, {ID: "model:FREE"}}, "model:FREE"},
		{"ollama local ordered patterns", api.OllamaLocalClientType, []api.ModelInfo{{ID: "llama3.1:8b"}, {ID: "llama3.2:3b"}}, "llama3.2:3b"},
		{"ollama cloud", api.OllamaCloudClientType, []api.ModelInfo{{ID: "deepseek"}, {ID: "gpt-oss:20b"}}, "gpt-oss:20b"},
		{"lmstudio skips embedding", api.LMStudioClientType, []api.ModelInfo{{ID: "text-embedding"}, {ID: "chat-model"}}, "chat-model"},
		{"default first", api.OpenAIClientType, []api.ModelInfo{{ID: "first"}, {ID: "second"}}, "first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.selectDefaultModel(tt.models, tt.provider); got != tt.want {
				t.Fatalf("selectDefaultModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	for _, tc := range []struct {
		id, pattern string
		want        bool
	}{
		{"Org/DeepSeek-Coder-Instruct", "deepseek*instruct", true},
		{"model:free", ":free", true},
		{"llama3.2:3b", "llama3.2", true},
		{"deepseek-chat", "deepseek*instruct", false},
		{"anything", "", false},
	} {
		if got := matchPattern(tc.id, tc.pattern); got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.id, tc.pattern, got, tc.want)
		}
	}
}
