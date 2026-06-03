package commands

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

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

func TestResolvePersona(t *testing.T) {
	tests := []struct {
		name      string
		config    *configuration.Config
		raw       string
		wantID    string
		wantName  string
		wantFound bool
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
			name:      "nil config",
			config:    nil,
			raw:       "coder",
			wantID:    "",
			wantName:  "",
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
