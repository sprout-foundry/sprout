package commands

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/stretchr/testify/assert"
)

func TestModelsCommandFindExactModel(t *testing.T) {
	cmd := &ModelsCommand{}

	tests := []struct {
		name    string
		models  []api.ModelInfo
		query   string
		wantID  string
		wantNil bool
	}{
		{
			name: "exact match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "gpt-3.5-turbo", Provider: "OpenAI"},
				{ID: "claude-3", Provider: "Anthropic"},
			},
			query:   "gpt-4",
			wantID:  "gpt-4",
			wantNil: false,
		},
		{
			name: "case insensitive match",
			models: []api.ModelInfo{
				{ID: "GPT-4", Provider: "OpenAI"},
				{ID: "gpt-3.5-turbo", Provider: "OpenAI"},
			},
			query:   "gpt-4",
			wantID:  "GPT-4",
			wantNil: false,
		},
		{
			name: "no match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "claude-3", Provider: "Anthropic"},
			},
			query:   "gemini-pro",
			wantID:  "",
			wantNil: true,
		},
		{
			name:    "empty models list",
			models:  []api.ModelInfo{},
			query:   "gpt-4",
			wantID:  "",
			wantNil: true,
		},
		{
			name: "empty query",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
			},
			query:   "",
			wantID:  "",
			wantNil: true,
		},
		{
			name:    "nil models list",
			models:  nil,
			query:   "gpt-4",
			wantID:  "",
			wantNil: true,
		},
		{
			name: "match with special characters",
			models: []api.ModelInfo{
				{ID: "openrouter/sono", Provider: "OpenRouter"},
			},
			query:   "openrouter/sono",
			wantID:  "openrouter/sono",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.findExactModel(tt.models, tt.query)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.wantID, got.ID)
			}
		})
	}
}

func TestModelsCommandFuzzySearchModels(t *testing.T) {
	cmd := &ModelsCommand{}

	tests := []struct {
		name    string
		models  []api.ModelInfo
		query   string
		wantLen int
		wantIDs []string
	}{
		{
			name: "substring match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "gpt-3.5-turbo", Provider: "OpenAI"},
				{ID: "claude-3-opus", Provider: "Anthropic"},
				{ID: "claude-3-sonnet", Provider: "Anthropic"},
			},
			query:   "gpt",
			wantLen: 2,
			wantIDs: []string{"gpt-4", "gpt-3.5-turbo"},
		},
		{
			name: "multi-word search",
			models: []api.ModelInfo{
				{ID: "openrouter/sono", Provider: "OpenRouter"},
				{ID: "openrouter/claude", Provider: "OpenRouter"},
			},
			query:   "openrouter/sono",
			wantLen: 1,
			wantIDs: []string{"openrouter/sono"},
		},
		{
			name: "empty query returns all",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "claude-3", Provider: "Anthropic"},
				{ID: "gemini-pro", Provider: "Google"},
			},
			query:   "",
			wantLen: 3,
			wantIDs: []string{"gpt-4", "claude-3", "gemini-pro"},
		},
		{
			name:    "no matches",
			models:  []api.ModelInfo{{ID: "gpt-4"}, {ID: "claude-3"}},
			query:   "xyz",
			wantLen: 0,
			wantIDs: []string{},
		},
		{
			name:    "empty models",
			models:  []api.ModelInfo{},
			query:   "gpt",
			wantLen: 0,
			wantIDs: []string{},
		},
		{
			name: "partial match with prefix bonus",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "gpt-3.5-turbo", Provider: "OpenAI"},
				{ID: "mini-gpt", Provider: "Other"},
			},
			query:   "gpt",
			wantLen: 3,
		},
		{
			name: "case insensitive",
			models: []api.ModelInfo{
				{ID: "GPT-4", Provider: "OpenAI"},
				{ID: "gpt-3.5-turbo", Provider: "OpenAI"},
			},
			query:   "GPT",
			wantLen: 2,
		},
		{
			name: "short word search (<3 chars)",
			models: []api.ModelInfo{
				{ID: "ai-model", Provider: "Test"},
				{ID: "other-model", Provider: "Test"},
			},
			query:   "ai", // Short words (<3 chars) don't match in description
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.fuzzySearchModels(tt.models, tt.query)
			assert.Equal(t, tt.wantLen, len(got), "should return expected number of results")

			if tt.wantIDs != nil && len(got) > 0 {
				// Extract IDs from results
				gotIDs := make([]string, len(got))
				for i, model := range got {
					gotIDs[i] = model.ID
				}
				// Just check that expected IDs are present (order may vary)
				for _, wantID := range tt.wantIDs {
					found := false
					for _, gotID := range gotIDs {
						if gotID == wantID {
							found = true
							break
						}
					}
					assert.True(t, found, "expected ID %q to be in results", wantID)
				}
			}
		})
	}
}

func TestModelsCommandCalculateFuzzyScore(t *testing.T) {
	cmd := &ModelsCommand{}

	tests := []struct {
		name  string
		model api.ModelInfo
		query string
		want  int
	}{
		{
			name: "exact substring match in ID",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "OpenAI's GPT-4 model",
			},
			query: "gpt",
			want:  190, // 100 (substring) + 50 (prefix) + 30 (word in ID >=3) + 10 (word in description)
		},
		{
			name: "substring match not at prefix",
			model: api.ModelInfo{
				ID: "mini-gpt",
			},
			query: "gpt",
			want:  130, // 100 (substring) + 30 (word in ID >=3), no prefix bonus
		},
		{
			name: "no match",
			model: api.ModelInfo{
				ID: "claude-3",
			},
			query: "gpt",
			want:  0,
		},
		{
			name: "case insensitive match",
			model: api.ModelInfo{
				ID: "GPT-4",
			},
			query: "gpt",
			want:  180, // 100 (substring) + 50 (prefix) + 30 (word in ID >=3)
		},
		{
			name: "description match (single word >=3 chars)",
			model: api.ModelInfo{
				ID:          "model-x",
				Description: "fast and efficient language model",
			},
			query: "language",
			want:  10, // 10 for description match only
		},
		{
			name: "multi-word query with slash",
			model: api.ModelInfo{
				ID: "openrouter/sono",
			},
			query: "openrouter/sono",
			want:  230, // 100 (substring) + 50 (prefix) + 80 (provider/model match)
		},
		{
			name: "short word (<3 chars) in description",
			model: api.ModelInfo{
				ID: "model-ai",
			},
			query: "ai",
			want:  100, // ID substring match only (short word skipped in word-by-word check)
		},
		{
			name: "empty query matches everything",
			model: api.ModelInfo{
				ID: "gpt-4",
			},
			query: "",
			want:  150, // 100 (Contains returns true for empty) + 50 (HasPrefix returns true for empty)
		},
		{
			name: "multiple words, some match",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "fast model",
			},
			query: "gpt fast",
			want:  40, // 30 (word "gpt" in ID) + 10 (word "fast" in description)
		},
		{
			name: "multi-word, one part in description",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "openai language model",
			},
			query: "gpt openai",
			want:  40, // 30 (word "gpt" in ID) + 10 (word "openai" in description)
		}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.calculateFuzzyScore(tt.model, tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelsCommandGetCurrentMatches(t *testing.T) {
	cmd := &ModelsCommand{}

	tests := []struct {
		name    string
		input   string
		models  []api.ModelInfo
		wantLen int
	}{
		{
			name:    "empty input returns all",
			input:   "",
			models:  []api.ModelInfo{{ID: "a"}, {ID: "b"}, {ID: "c"}},
			wantLen: 3,
		},
		{
			name:  "matching input filters",
			input: "gpt",
			models: []api.ModelInfo{
				{ID: "gpt-4"},
				{ID: "gpt-3.5"},
				{ID: "claude-3"},
			},
			wantLen: 2,
		},
		{
			name:  "no matches returns empty",
			input: "xyz",
			models: []api.ModelInfo{
				{ID: "gpt-4"},
				{ID: "claude-3"},
			},
			wantLen: 0,
		},
		{
			name:    "empty models returns empty",
			input:   "gpt",
			models:  []api.ModelInfo{},
			wantLen: 0,
		},
		{
			name:    "nil models returns empty",
			input:   "gpt",
			models:  nil,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.getCurrentMatches(tt.input, tt.models)
			assert.Equal(t, tt.wantLen, len(got))
		})
	}
}

func TestModelsCommandCommonPrefix(t *testing.T) {
	cmd := &ModelsCommand{}

	tests := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "common prefix",
			a:    "gpt-4",
			b:    "gpt-3.5",
			want: "gpt-",
		},
		{
			name: "no common prefix",
			a:    "gpt-4",
			b:    "claude-3",
			want: "",
		},
		{
			name: "identical strings",
			a:    "gpt-4",
			b:    "gpt-4",
			want: "gpt-4",
		},
		{
			name: "one is prefix of other",
			a:    "gpt",
			b:    "gpt-4",
			want: "gpt",
		},
		{
			name: "case insensitive returns original case of first arg",
			a:    "GPT-4",
			b:    "gpt-3.5",
			want: "GPT-", // Returns prefix from 'a' preserving case
		},
		{
			name: "empty strings",
			a:    "",
			b:    "",
			want: "",
		},
		{
			name: "one empty string",
			a:    "gpt-4",
			b:    "",
			want: "",
		},
		{
			name: "single char common",
			a:    "apple",
			b:    "banana",
			want: "",
		},
		{
			name: "almost identical",
			a:    "gpt-4-turbo",
			b:    "gpt-4-turbo-v2",
			want: "gpt-4-turbo",
		},
		{
			name: "different lengths",
			a:    "a",
			b:    "abc",
			want: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.commonPrefix(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelsCommandFindFeaturedModels(t *testing.T) {
	cmd := &ModelsCommand{}

	// Test that featured models concept has been removed
	tests := []struct {
		name       string
		models     []api.ModelInfo
		clientType api.ClientType
		wantEmpty  bool
	}{
		{
			name: "multiple models",
			models: []api.ModelInfo{
				{ID: "gpt-4", Provider: "OpenAI"},
				{ID: "claude-3", Provider: "Anthropic"},
				{ID: "gemini-pro", Provider: "Google"},
			},
			clientType: api.OpenAIClientType,
			wantEmpty:  true,
		},
		{
			name:       "empty models list",
			models:     []api.ModelInfo{},
			clientType: api.OpenAIClientType,
			wantEmpty:  true,
		},
		{
			name:       "nil models list",
			models:     nil,
			clientType: api.OpenAIClientType,
			wantEmpty:  true,
		},
		{
			name: "different provider types",
			models: []api.ModelInfo{
				{ID: "llama-3", Provider: "Ollama"},
				{ID: "gpt-4", Provider: "OpenAI"},
			},
			clientType: api.OllamaClientType,
			wantEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.findFeaturedModels(tt.models, tt.clientType)
			if tt.wantEmpty {
				assert.Empty(t, got, "should return empty list")
			} else {
				assert.NotEmpty(t, got)
			}
		})
	}
}
