package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWorkspaceMCPConfig writes an mcp_config.json under the workspace's
// .sprout directory.
func writeWorkspaceMCPConfig(t *testing.T, workspace string, servers map[string]mcp.MCPServerConfig) {
	t.Helper()
	dir := filepath.Join(workspace, ".sprout")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := json.MarshalIndent(mcp.MCPConfig{Enabled: true, Servers: servers}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp_config.json"), data, 0o600))
}

// A workspace's MCP servers must reach the agent that serves that
// workspace, even when the process's config root points elsewhere. That is
// the daemon case: the daemon's SPROUT_CONFIG was fixed when it started, so
// resolving the config file through the ambient env hid a repo-local
// mcp_config.json written afterwards. The agent's own workspace root is the
// correct source.
func TestInitializeMCP_ReadsWorkspaceConfigNotProcessRoot(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	agent := newTestAgent(t)
	// newTestAgent points SPROUT_CONFIG at its own isolated dir. Put a
	// DIFFERENT server there, so the test proves the workspace file is what
	// was read rather than the process root.
	processRoot := os.Getenv("SPROUT_CONFIG")
	require.NotEmpty(t, processRoot)
	processCfg := mcp.MCPConfig{
		Enabled: true,
		Servers: map[string]mcp.MCPServerConfig{
			"process-root": {Name: "process-root", Command: "process-cmd", Type: "stdio"},
		},
	}
	data, err := json.MarshalIndent(processCfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(processRoot, "mcp_config.json"), data, 0o600))

	workspace := t.TempDir()
	writeWorkspaceMCPConfig(t, workspace, map[string]mcp.MCPServerConfig{
		"repo-local": {Name: "repo-local", Command: "repo-cmd", Type: "stdio"},
	})
	agent.SetWorkspaceRoot(workspace)

	require.NoError(t, agent.initializeMCP())

	names := mcpServerNames(agent)
	assert.Contains(t, names, "repo-local",
		"the agent must see its workspace's MCP servers regardless of the process config root")
	// Both layers are consulted, so the process-root server is present too;
	// the point is that the workspace one is not lost.
	assert.Contains(t, names, "process-root")
}

// A globally-configured server stays visible when the workspace has no MCP
// config of its own — the workspace layer is additive, not exclusive.
func TestInitializeMCP_GlobalServersStillVisible(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	agent := newTestAgent(t)
	// newTestAgent points SPROUT_CONFIG at its own isolated dir; write the
	// global MCP config there so it is the process root the agent reads.
	processRoot := os.Getenv("SPROUT_CONFIG")
	require.NotEmpty(t, processRoot)

	globalCfg := mcp.MCPConfig{
		Enabled: true,
		Servers: map[string]mcp.MCPServerConfig{
			"global-one": {Name: "global-one", Command: "global-cmd", Type: "stdio"},
		},
	}
	data, err := json.MarshalIndent(globalCfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(processRoot, "mcp_config.json"), data, 0o600))

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	require.NoError(t, agent.initializeMCP())

	assert.Contains(t, mcpServerNames(agent), "global-one")
}

// mcpServerNames lists the servers the agent's MCP manager knows about.
func mcpServerNames(a *Agent) []string {
	if a.mcpSub == nil || a.mcpSub.GetManager() == nil {
		return nil
	}
	servers := a.mcpSub.GetManager().ListServers()
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.GetName())
	}
	return names
}

// The config-root helper the agent relies on must resolve the workspace
// layer, so the "no MCP servers" report can name the file consulted.
func TestMCPConfigSourcePathForAgentWorkspace(t *testing.T) {
	processRoot := t.TempDir()
	t.Setenv("SPROUT_CONFIG", processRoot)

	workspace := t.TempDir()
	writeWorkspaceMCPConfig(t, workspace, map[string]mcp.MCPServerConfig{
		"s": {Name: "s", Command: "cmd", Type: "stdio"},
	})

	got := mcp.MCPConfigSourcePath(workspace)
	assert.Equal(t, filepath.Join(workspace, ".sprout", "mcp_config.json"), got)
}

// The agent-construction path used by the daemon must carry the workspace
// root into the agent, or the workspace-scoped MCP load has nothing to
// scope to.
func TestNewAgentWithLayersInWorkspace_CarriesWorkspaceRoot(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	cm, err := configuration.NewManagerWithLayers(globalDir, "")
	require.NoError(t, err)

	a, err := newAgentWithConfigManagerAndWorkspace(cm, workspace, "")
	require.NoError(t, err)
	t.Cleanup(a.Shutdown)

	assert.Equal(t, workspace, a.GetWorkspaceRoot())
}
