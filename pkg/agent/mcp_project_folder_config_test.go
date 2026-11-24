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

// writeProjectFolderMCPConfig writes an mcp_config.json under the project folder's
// .sprout directory.
func writeProjectFolderMCPConfig(t *testing.T, projectFolder string, servers map[string]mcp.MCPServerConfig) {
	t.Helper()
	dir := filepath.Join(projectFolder, ".sprout")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := json.MarshalIndent(mcp.MCPConfig{Enabled: true, Servers: servers}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp_config.json"), data, 0o600))
}

// A project folder's MCP servers must reach the agent that serves that
// project folder, even when the process's config root points elsewhere. That is
// the daemon case: the daemon's SPROUT_CONFIG was fixed when it started, so
// resolving the config file through the ambient env hid a repo-local
// mcp_config.json written afterwards. The agent's own project folder is the
// correct source.
func TestInitializeMCP_ReadsProjectFolderConfigNotProcessRoot(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	agent := newTestAgent(t)
	// newTestAgent points SPROUT_CONFIG at its own isolated dir. Put a
	// DIFFERENT server there, so the test proves the project folder file is what
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

	projectFolder := t.TempDir()
	writeProjectFolderMCPConfig(t, projectFolder, map[string]mcp.MCPServerConfig{
		"repo-local": {Name: "repo-local", Command: "repo-cmd", Type: "stdio"},
	})
	agent.SetProjectFolder(projectFolder)

	require.NoError(t, agent.initializeMCP())

	names := mcpServerNames(agent)
	assert.Contains(t, names, "repo-local",
		"the agent must see its project folder's MCP servers regardless of the process config root")
	// Both layers are consulted, so the process-root server is present too;
	// the point is that the project folder one is not lost.
	assert.Contains(t, names, "process-root")
}

// A globally-configured server stays visible when the project folder has no MCP
// config of its own — the project folder layer is additive, not exclusive.
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

	projectFolder := t.TempDir()
	agent.SetProjectFolder(projectFolder)

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

// The config-root helper the agent relies on must resolve the project folder
// layer, so the "no MCP servers" report can name the file consulted.
func TestMCPConfigSourcePathForAgentProjectFolder(t *testing.T) {
	processRoot := t.TempDir()
	t.Setenv("SPROUT_CONFIG", processRoot)

	projectFolder := t.TempDir()
	writeProjectFolderMCPConfig(t, projectFolder, map[string]mcp.MCPServerConfig{
		"s": {Name: "s", Command: "cmd", Type: "stdio"},
	})

	got := mcp.MCPConfigSourcePath(projectFolder)
	assert.Equal(t, filepath.Join(projectFolder, ".sprout", "mcp_config.json"), got)
}

// The agent-construction path used by the daemon must carry the project folder
// root into the agent, or the project-folder-scoped MCP load has nothing to
// scope to.
func TestNewAgentWithLayersInProjectFolder_CarriesProjectFolder(t *testing.T) {
	globalDir := t.TempDir()
	projectFolder := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	cm, err := configuration.NewManagerWithLayers(globalDir, "")
	require.NoError(t, err)

	a, err := newAgentWithConfigManagerAndProjectFolder(cm, projectFolder, "")
	require.NoError(t, err)
	t.Cleanup(a.Shutdown)

	assert.Equal(t, projectFolder, a.GetProjectFolder())
}

// A remote server's headers reach the manager: an HTTP server whose auth
// header was dropped on load could never authenticate.
func TestInitializeMCP_KeepsServerHeaders(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	agent := newTestAgent(t)
	projectFolder := t.TempDir()
	writeProjectFolderMCPConfig(t, projectFolder, map[string]mcp.MCPServerConfig{
		"remote": {Name: "remote", Type: "http", URL: "https://mcp.example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer {{credential:mcp/remote/TOKEN}}"}},
	})
	agent.SetProjectFolder(projectFolder)
	require.NoError(t, agent.initializeMCP())

	srv, ok := agent.mcpSub.GetManager().GetServer("remote")
	require.True(t, ok)
	assert.Equal(t, "Bearer {{credential:mcp/remote/TOKEN}}", srv.GetConfig().Headers["Authorization"])
}
