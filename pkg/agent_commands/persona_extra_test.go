package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestParseCommaListExtra(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantItems []string
	}{
		{
			name:      "simple comma-separated",
			raw:       "apple,banana,cherry",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "with spaces",
			raw:       "apple, banana, cherry",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "empty string",
			raw:       "",
			wantItems: []string{},
		},
		{
			name:      "single item",
			raw:       "apple",
			wantItems: []string{"apple"},
		},
		{
			name:      "duplicates removed",
			raw:       "apple,banana,apple,cherry,banana",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "empty items removed",
			raw:       "apple,,banana,,cherry",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "whitespace items removed",
			raw:       "apple,   ,banana,   ,cherry",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "trailing comma",
			raw:       "apple,banana,cherry,",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "leading comma",
			raw:       ",apple,banana,cherry",
			wantItems: []string{"apple", "banana", "cherry"},
		},
		{
			name:      "only commas",
			raw:       ",,,",
			wantItems: []string{},
		},
		{
			name:      "mixed case",
			raw:       "Apple,BANANA,Cherry",
			wantItems: []string{"Apple", "BANANA", "Cherry"},
		},
		{
			name:      "numbers",
			raw:       "1,2,3,4,5",
			wantItems: []string{"1", "2", "3", "4", "5"},
		},
		{
			name:      "special characters",
			raw:       "read_file,search_files,TodoWrite",
			wantItems: []string{"read_file", "search_files", "TodoWrite"},
		},
		{
			name:      "single item with spaces",
			raw:       " apple ",
			wantItems: []string{"apple"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCommaList(tt.raw)
			assert.Equal(t, tt.wantItems, got)
		})
	}
}

func TestNormalizePersonaKeyExtra(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "simple lowercase",
			raw:  "Coder",
			want: "coder",
		},
		{
			name: "trim whitespace",
			raw:  "  Coder  ",
			want: "coder",
		},
		{
			name: "dashes to underscores",
			raw:  "git-commit",
			want: "git_commit",
		},
		{
			name: "multiple dashes",
			raw:  "git-commit-helper",
			want: "git_commit_helper",
		},
		{
			name: "already underscores",
			raw:  "git_commit",
			want: "git_commit",
		},
		{
			name: "mixed dashes and underscores",
			raw:  "git_commit-helper",
			want: "git_commit_helper",
		},
		{
			name: "uppercase with dashes",
			raw:  "Git-Commit-Helper",
			want: "git_commit_helper",
		},
		{
			name: "numbers preserved",
			raw:  "model-3_v2",
			want: "model_3_v2",
		},
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "only whitespace",
			raw:  "   ",
			want: "",
		},
		{
			name: "only dashes",
			raw:  "---",
			want: "___",
		},
		{
			name: "only underscores",
			raw:  "___",
			want: "___",
		},
		{
			name: "spaces preserved unchanged",
			raw:  "git commit helper",
			want: "git commit helper",
		},
		{
			name: "mixed case, dashes, underscores",
			raw:  "My-Git_Commit Helper",
			want: "my_git_commit helper",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePersonaKey(tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPersonaTitle(t *testing.T) {
	tests := []struct {
		name     string
		personaID string
		want     string
	}{
		{
			name:     "simple underscore",
			personaID: "git_commit",
			want:     "Git Commit",
		},
		{
			name:     "multiple words",
			personaID: "code_reviewer",
			want:     "Code Reviewer",
		},
		{
			name:     "single word",
			personaID: "coder",
			want:     "Coder",
		},
		{
			name:     "already title case",
			personaID: "Git_Commit",
			want:     "Git Commit",
		},
		{
			name:     "all uppercase",
			personaID: "GIT_COMMIT",
			want:     "GIT COMMIT",
		},
		{
			name:     "numbers",
			personaID: "model_3_v2",
			want:     "Model 3 V2",
		},
		{
			name:     "single letter words",
			personaID: "a_b_c",
			want:     "A B C",
		},
		{
			name:     "empty string",
			personaID: "",
			want:     "",
		},
		{
			name:     "single underscore",
			personaID: "a",
			want:     "A",
		},
		{
			name:     "trailing underscore",
			personaID: "coder_",
			want:     "Coder", // Trailing underscore creates empty word, which gets filtered
		},
		{
			name:     "leading underscore",
			personaID: "_coder",
			want:     "Coder", // Leading underscore creates empty word, which gets filtered
		},
		{
			name:     "multiple underscores",
			personaID: "code_reviewer_helper",
			want:     "Code Reviewer Helper",
		},
		{
			name:     "mixed with numbers",
			personaID: "go_1_21_helper",
			want:     "Go 1 21 Helper",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := personaTitle(tt.personaID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCustomPersonaTemplateExtra(t *testing.T) {
	tests := []struct {
		name      string
		personaID string
		wantName  string
	}{
		{
			name:      "simple ID",
			personaID: "my_coder",
			wantName:  "My Coder",
		},
		{
			name:      "with dashes",
			personaID: "git-helper",
			wantName:  "Git Helper",
		},
		{
			name:      "uppercase",
			personaID: "HELPER",
			wantName:  "HELPER", // title case applied to already-uppercase
		},
		{
			name:      "with numbers",
			personaID: "model_3",
			wantName:  "Model 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCustomPersonaTemplate(tt.personaID)

			// Verify basic properties
			assert.Equal(t, tt.personaID, got.ID) // ID is used directly, not normalized
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, "Custom persona", got.Description)
			assert.True(t, got.Enabled)

			// Verify default tools are set
			assert.NotNil(t, got.AllowedTools)
			assert.Greater(t, len(got.AllowedTools), 0)

			// Verify default tools include expected ones
			toolSet := make(map[string]bool)
			for _, tool := range got.AllowedTools {
				toolSet[tool] = true
			}
			assert.True(t, toolSet["read_file"], "should include read_file")
			assert.True(t, toolSet["search_files"], "should include search_files")
		})
	}
}

func TestResolvePersona(t *testing.T) {
	tests := []struct {
		name         string
		config       *configuration.Config
		raw          string
		wantID       string
		wantName     string
		wantFound    bool
	}{
		{
			name: "exact ID match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "coder",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "case insensitive ID match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "CODER",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "exact name match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "code helper",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "case insensitive name match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "CODE HELPER",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "alias match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:      "coder",
						Name:    "Code Helper",
						Aliases: []string{"dev", "helper"},
					},
				},
			},
			raw:       "helper",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "case insensitive alias match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:      "coder",
						Name:    "Code Helper",
						Aliases: []string{"dev", "helper"},
					},
				},
			},
			raw:       "DEV",
			wantID:    "coder",
			wantName:  "Code Helper",
			wantFound: true,
		},
		{
			name: "no match",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "nonexistent",
			wantID:    "",
			wantName:  "",
			wantFound: false,
		},
		{
			name:   "nil config",
			config: nil,
			raw:    "coder",
			wantID:  "",
			wantName: "",
			wantFound: false,
		},
		{
			name: "empty subagent types",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{},
			},
			raw:       "coder",
			wantID:    "",
			wantName:  "",
			wantFound: false,
		},
		{
			name: "empty string input",
			config: &configuration.Config{
				SubagentTypes: map[string]configuration.SubagentType{
					"coder": {
						ID:   "coder",
						Name: "Code Helper",
					},
				},
			},
			raw:       "",
			wantID:    "",
			wantName:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotPersona, gotFound := resolvePersona(tt.config, tt.raw)
			assert.Equal(t, tt.wantFound, gotFound)
			if tt.wantFound {
				assert.Equal(t, tt.wantID, gotID)
				assert.Equal(t, tt.wantName, gotPersona.Name)
			} else {
				assert.Empty(t, gotID)
				assert.Nil(t, gotPersona)
			}
		})
	}
}
