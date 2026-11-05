package agent

import (
	"strings"
	"testing"
)

// With servers registered, a name that matches none of them must be refused.
// Guessing at the first underscore is how a call gets authorized against one
// server while running on another, which is the whole point of the allowlist.
func TestSplitMCPToolName_FailsClosedOnUnresolvableName(t *testing.T) {
	names := []string{"acme", "my_server"}

	_, _, err := splitMCPToolName("mcp_unknown_read", "unknown_read", names)
	if err == nil {
		t.Fatal("expected an error for a name that matches no registered server")
	}
	if !strings.Contains(err.Error(), "Refusing to guess") {
		t.Errorf("error does not explain the refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "mcp_unknown_read") {
		t.Errorf("error does not name the tool: %v", err)
	}
}

// With nothing registered there is no identity to confuse, so the historical
// split stands and the manager reports the missing server.
func TestSplitMCPToolName_EmptyRegistryKeepsHistoricalSplit(t *testing.T) {
	server, tool, err := splitMCPToolName("mcp_acme_read", "acme_read", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server != "acme" || tool != "read" {
		t.Errorf("got (%q, %q), want (acme, read)", server, tool)
	}
}

// The resolution still prefers the longest configured name, which is the case
// that made fail-closed necessary in the first place.
func TestSplitMCPToolName_PrefersLongestServer(t *testing.T) {
	names := []string{"my", "my_server"}

	server, tool, err := splitMCPToolName("mcp_my_server_read", "my_server_read", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server != "my_server" || tool != "read" {
		t.Errorf("got (%q, %q), want (my_server, read)", server, tool)
	}
}

func TestSplitMCPToolName_EmptyName(t *testing.T) {
	if _, _, err := splitMCPToolName("mcp_", "", nil); err == nil {
		t.Fatal("expected an error for an empty tool name")
	}
}
