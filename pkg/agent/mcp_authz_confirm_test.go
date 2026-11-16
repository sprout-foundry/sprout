package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// A tool its server marks confirmEachCall is asked about on every call: an
// allowlist entry does not run it unasked, so with no one to ask it is
// denied, and the prompt offers no standing allow.
func TestMCPAuthz_ConfirmEachCallIgnoresTheAllowlist(t *testing.T) {
	a, mgr := authzTestAgent(t, "sprout-foundry")
	sub, ok := a.mcpSub.(*AgentMCPManager)
	if !ok {
		t.Fatalf("mcpSub is %T", a.mcpSub)
	}
	sub.setConfirmEachCall([]mcp.MCPTool{
		{ServerName: "sprout-foundry", Name: "report_platform_issue", Annotations: map[string]interface{}{"confirmEachCall": true}},
		{ServerName: "sprout-foundry", Name: "list_projects", Annotations: map[string]interface{}{"readOnlyHint": true}},
	})

	_, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "sprout-foundry", "tool": "report_platform_issue",
		"arguments": map[string]interface{}{"title": "Diff fails"},
	})
	if err == nil || mgr.callCount != 0 {
		t.Fatalf("an allowlisted confirm-each-call tool ran unasked (err=%v, calls=%d)", err, mgr.callCount)
	}
	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "sprout-foundry", "tool": "list_projects",
	}); err != nil || mgr.callCount != 1 {
		t.Fatalf("an ordinary allowlisted tool should still run (err=%v, calls=%d)", err, mgr.callCount)
	}

	req := mcpAuthzRequest{server: "sprout-foundry", tool: "report_platform_issue", confirmEach: true,
		args: map[string]interface{}{"description": strings.Repeat("x", 3000)}}
	for _, item := range mcpAuthzItems(req) {
		if item.Value == mcpAuthzAllowTool || item.Value == mcpAuthzAllowServer {
			t.Errorf("a confirm-each-call prompt offers %q", item.Label)
		}
	}
	if preview := req.argsPreview(); strings.Contains(preview, "(cut)") || !strings.Contains(preview, strings.Repeat("x", 3000)) {
		t.Error("a confirm-each-call prompt shows the arguments whole")
	}
	req.confirmEach = false
	if preview := req.argsPreview(); !strings.Contains(preview, "(cut)") {
		t.Error("an ordinary prompt cuts long arguments")
	}
	if len(mcpAuthzItems(req)) != 4 {
		t.Error("an ordinary prompt offers the four answers")
	}
}

func TestMCPToolConfirmEachCallAnnotation(t *testing.T) {
	if !(mcp.MCPTool{Annotations: map[string]interface{}{"confirmEachCall": true}}).ConfirmEachCall() {
		t.Error("confirmEachCall: true is read")
	}
	if (mcp.MCPTool{}).ConfirmEachCall() || (mcp.MCPTool{Annotations: map[string]interface{}{"confirmEachCall": "yes"}}).ConfirmEachCall() {
		t.Error("only a boolean true counts")
	}
}
