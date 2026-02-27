package configuration

import "testing"

func TestUnknownPersonaTools(t *testing.T) {
	tests := []struct {
		name     string
		tools    []string
		wantNone bool
	}{
		{
			name:     "all known tools",
			tools:    []string{"read_file", "search_files", "git", "run_parallel_subagents", "list_skills", "activate_skill"},
			wantNone: true,
		},
		{
			name:     "mcp direct tool names are allowed",
			tools:    []string{"mcp_tools", "mcp_github_create_issue"},
			wantNone: true,
		},
		{
			name:     "unknown tools are detected",
			tools:    []string{"read_file", "not_a_real_tool", "another_fake_tool"},
			wantNone: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unknown := UnknownPersonaTools(tt.tools)
			if tt.wantNone && len(unknown) != 0 {
				t.Fatalf("expected no unknown tools, got %v", unknown)
			}
			if !tt.wantNone && len(unknown) == 0 {
				t.Fatalf("expected unknown tools, got none")
			}
		})
	}
}
