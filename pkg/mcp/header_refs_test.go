package mcp

import "testing"

func TestResolveHeaderRefsInsideText(t *testing.T) {
	t.Setenv("SPROUT_TEST_MCP_TOKEN", "tok-123")
	got, ok := resolveHeaderRefs("srv", "Authorization", "Bearer {{credential:mcp/srv/SPROUT_TEST_MCP_TOKEN}}")
	if !ok || got != "Bearer tok-123" {
		t.Fatalf("got %q, %v", got, ok)
	}
	got, ok = resolveHeaderRefs("srv", "X-Key", "{{credential:mcp/srv/SPROUT_TEST_MCP_TOKEN}}")
	if !ok || got != "tok-123" {
		t.Fatalf("whole-value placeholder: got %q, %v", got, ok)
	}
	got, ok = resolveHeaderRefs("srv", "X-Plain", "plain value")
	if !ok || got != "plain value" {
		t.Fatalf("plain value: got %q, %v", got, ok)
	}
	if _, ok := resolveHeaderRefs("srv", "Authorization", "Bearer {{credential:mcp/srv/SPROUT_TEST_MCP_UNSET}}"); ok {
		t.Fatal("a placeholder whose credential is unset drops the header")
	}
}
