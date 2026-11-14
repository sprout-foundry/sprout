package agent

import "testing"

func TestToolAllowedWildcard(t *testing.T) {
	allowed := makeAllowedToolSet([]string{"read_file", "mcp_*", "web_*"})
	for name, want := range map[string]bool{
		"read_file":            true,
		"write_file":           false,
		"mcp_github_get_issue": true,
		"web_search":           true,
		"webby":                false,
		"mcp":                  false,
	} {
		if got := toolAllowed(allowed, name); got != want {
			t.Errorf("toolAllowed(%q) = %v, want %v", name, got, want)
		}
	}
	server := makeAllowedToolSet([]string{"mcp_linear_*"})
	if toolAllowed(server, "mcp_github_x") || !toolAllowed(server, "mcp_linear_list") {
		t.Error("a server's entry admits only that server's tools")
	}
}
