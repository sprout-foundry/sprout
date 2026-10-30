package injection

import "testing"

// TestAllowlistedMCPTool_Matching covers the two entry forms and the
// whole-segment rule that makes the allowlist safe: `acme` must not
// authorize `acmecorp`, and a tool entry must not cover its siblings.
func TestAllowlistedMCPTool_Matching(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		server  string
		tool    string
		want    bool
	}{
		{"no entries", nil, "github", "read", false},
		{"server entry covers any tool", []string{"github"}, "github", "delete_repo", true},
		{"server entry covers another tool", []string{"github"}, "github", "read", true},
		{"tool entry covers that tool", []string{"github__read"}, "github", "read", true},
		{"tool entry does not cover a sibling", []string{"github__read"}, "github", "delete_repo", false},
		{"tool entry does not cover other servers", []string{"github__read"}, "gitlab", "read", false},
		{"whole-segment: acme does not match acmecorp", []string{"acme"}, "acmecorp", "read", false},
		{"whole-segment: acmecorp does not match acme", []string{"acmecorp"}, "acme", "read", false},
		{"whole-segment: tool prefix does not match", []string{"acme__read"}, "acme", "read_file", false},
		{"case-sensitive server", []string{"GitHub"}, "github", "read", false},
		{"case-sensitive tool", []string{"github__Read"}, "github", "read", false},
		{"underscore in server name", []string{"my_server__read"}, "my_server", "read", true},
		{"underscore in server name does not leak to prefix", []string{"my_server__read"}, "my", "server_read", false},
		{"empty server matches nothing", []string{"github"}, "", "read", false},
		{"empty tool matches nothing", []string{"github"}, "github", "", false},
		{"empty entry ignored", []string{""}, "github", "read", false},
		{"second entry matches", []string{"gitlab", "github__read"}, "github", "read", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowlistedMCPTool(tt.entries, tt.server, tt.tool); got != tt.want {
				t.Errorf("AllowlistedMCPTool(%v, %q, %q) = %v, want %v",
					tt.entries, tt.server, tt.tool, got, tt.want)
			}
		})
	}
}

// TestMCPToolAllowlistEntry pins the entry the denial message names.
func TestMCPToolAllowlistEntry(t *testing.T) {
	if got := MCPToolAllowlistEntry("github", "delete_repo"); got != "github__delete_repo" {
		t.Errorf("MCPToolAllowlistEntry = %q, want %q", got, "github__delete_repo")
	}
	if got := MCPToolAllowlistServerEntry("github"); got != "github" {
		t.Errorf("MCPToolAllowlistServerEntry = %q, want %q", got, "github")
	}
}
