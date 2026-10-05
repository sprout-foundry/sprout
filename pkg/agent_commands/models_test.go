package commands

import (
	"bytes"
	"io"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// ====================================================================
// /model --role (SP-150 §150d, item 150.6)
// ====================================================================

func TestModelsCommandExecute_RoleModelPersists(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NotNil(t, cm)

	var buf bytes.Buffer
	cmd := &ModelsCommand{}
	cmd.SetOutput(&buf)

	require.NoError(t, cmd.Execute([]string{"--role", "planner", "some-model"}, chatAgent))

	// The role's model is persisted through the config manager.
	assert.Equal(t, "some-model", cm.GetConfig().GetRole(configuration.RolePlanner).Model)
	assert.Contains(t, buf.String(), `Role "planner" model set to: some-model`)

	// The active conversation model is untouched by the role path.
	assert.NotContains(t, buf.String(), "Model set to: ")
}

func TestModelsCommandExecute_RoleModelPreservesProvider(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NotNil(t, cm)

	// A stored provider on the role must survive a model update.
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetRole(configuration.RolePlanner, configuration.RoleConfig{Provider: "zai"})
		return nil
	}))

	var buf bytes.Buffer
	cmd := &ModelsCommand{}
	cmd.SetOutput(&buf)

	require.NoError(t, cmd.Execute([]string{"--role", "planner", "glm-4"}, chatAgent))

	stored := cm.GetConfig().GetRole(configuration.RolePlanner)
	assert.Equal(t, "zai", stored.Provider, "stored provider must be preserved")
	assert.Equal(t, "glm-4", stored.Model)
	assert.Contains(t, buf.String(), `Role "planner" model set to: glm-4 (provider: zai)`)
}

func TestModelsCommandExecute_RoleModelValidation(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NotNil(t, cm)

	cmd := &ModelsCommand{}
	cmd.SetOutput(io.Discard)

	// Missing model ID.
	require.Error(t, cmd.Execute([]string{"--role", "planner"}, chatAgent))
	// Missing role name (flag at the end of the arguments).
	require.Error(t, cmd.Execute([]string{"some-model", "--role"}, chatAgent))
	// Whitespace in the role name is rejected.
	require.Error(t, cmd.Execute([]string{"--role", "pl anner", "m"}, chatAgent))

	// Unknown role names are accepted (the Roles map takes arbitrary
	// names — the SP-150 open question of user-defined roles).
	require.NoError(t, cmd.Execute([]string{"--role", "my-custom-role", "m"}, chatAgent))
	assert.Equal(t, "m", cm.GetConfig().GetRole("my-custom-role").Model)
}

func TestParseRoleArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantRole  string
		wantModel string
		wantFound bool
		wantErr   bool
	}{
		{name: "no args has no flag", args: []string{}, wantFound: false},
		{name: "bare model ID has no flag", args: []string{"gpt-4o"}, wantFound: false},
		{name: "select has no flag", args: []string{"select"}, wantFound: false},
		{name: "flag first", args: []string{"--role", "planner", "gpt-4o"}, wantRole: "planner", wantModel: "gpt-4o", wantFound: true},
		{name: "flag last", args: []string{"gpt-4o", "--role", "planner"}, wantRole: "planner", wantModel: "gpt-4o", wantFound: true},
		{name: "missing role name", args: []string{"--role"}, wantErr: true},
		{name: "missing model", args: []string{"--role", "planner"}, wantErr: true},
		{name: "extra non-flag args", args: []string{"--role", "planner", "gpt-4o", "extra"}, wantErr: true},
		// Repeated flags are malformed: the first flag's value is a
		// non-flag argument, so the model list is no longer exactly one.
		{name: "repeated flags", args: []string{"--role", "planner", "--role", "coder", "gpt-4o"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, model, found, err := parseRoleArgs(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFound, found)
			if found {
				assert.Equal(t, tt.wantRole, role)
				assert.Equal(t, tt.wantModel, model)
			}
		})
	}
}

// TestModelsCommandExecute_NoRoleFlagUnchanged pins the pre-role dispatch:
// without --role the existing paths are unchanged (multi-arg usage error,
// bare ID routed to the model set path, not the role path).
func TestModelsCommandExecute_NoRoleFlagUnchanged(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &ModelsCommand{}
	cmd.SetOutput(io.Discard)
	// Two bare arguments (no --role) is still a usage error.
	require.Error(t, cmd.Execute([]string{"gpt-4o", "extra"}, chatAgent))

	// A bare model ID is not treated as a role flag.
	role, model, found, err := parseRoleArgs([]string{"gpt-4o"})
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, role)
	assert.Empty(t, model)
}
