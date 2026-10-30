package agent

import "testing"

// The advertised name is "mcp_<server>_<tool>". Splitting at the first
// underscore reads a server whose name contains one as a different server, and
// with an allowlist in play that would authorize the wrong thing — so the
// configured server names decide the split, longest first.
func TestResolveMCPToolIdentityFromNames(t *testing.T) {
	names := []string{"my", "my_server", "acme"}

	cases := []struct {
		rest       string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		// The bug this exists for: the longer server name must win.
		{"my_server_read", "my_server", "read", true},
		{"my_read", "my", "read", true},
		// A plain server still resolves.
		{"acme_delete_repo", "acme", "delete_repo", true},
		// A tool name may itself contain underscores.
		{"my_server_get_issue", "my_server", "get_issue", true},
		// No configured server matches: the caller falls back.
		{"unknown_tool", "", "", false},
		// A shared byte prefix is not a segment prefix.
		{"acmecorp_delete", "", "", false},
		// A server name that is only a prefix of the remainder without the
		// separating underscore is not a match.
		{"myserver_read", "", "", false},
	}

	for _, tc := range cases {
		server, tool, ok := resolveMCPToolIdentityFromNames(tc.rest, names)
		if ok != tc.wantOK || server != tc.wantServer || tool != tc.wantTool {
			t.Errorf("resolveMCPToolIdentityFromNames(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.rest, server, tool, ok, tc.wantServer, tc.wantTool, tc.wantOK)
		}
	}
}

// With no configured servers the historical split still applies, so a tool whose
// server is not running yet reports the same identity it always did.
func TestResolveMCPToolIdentityFromNames_Empty(t *testing.T) {
	if _, _, ok := resolveMCPToolIdentityFromNames("acme_read", nil); ok {
		t.Error("expected no match with no configured servers")
	}
}
