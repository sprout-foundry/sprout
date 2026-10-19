package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMCPLayer writes an mcp_config.json into dir.
func writeMCPLayer(t *testing.T, dir string, cfg MCPConfig) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, mcpConfigFileName), data, 0o600))
}

func serverConfig(name, command string) MCPServerConfig {
	return MCPServerConfig{Name: name, Command: command}
}

// A repo-local mcp_config.json must be visible to an agent serving that
// repo, whichever process constructed the agent. Before this, the load
// resolved the process's ambient config root, so a daemon whose root was
// fixed at startup never saw a workspace file written afterwards.
func TestLoadMCPConfigForWorkspace_SeesWorkspaceServers(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_ENABLED", "")
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	writeMCPLayer(t, filepath.Join(workspace, ".sprout"), MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{"repo-local": serverConfig("repo-local", "repo-cmd")},
	})

	cfg, err := LoadMCPConfigForWorkspace(workspace)
	require.NoError(t, err)
	assert.Contains(t, cfg.Servers, "repo-local")
	assert.Equal(t, "repo-cmd", cfg.Servers["repo-local"].Command)
}

// The workspace layer wins on a name collision; the global layer still
// contributes servers the workspace does not define.
func TestLoadMCPConfigForWorkspace_WorkspaceWinsCollision(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_ENABLED", "")
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	writeMCPLayer(t, globalDir, MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"shared": serverConfig("shared", "global-cmd"),
			"global": serverConfig("global", "global-only-cmd"),
		},
	})
	writeMCPLayer(t, filepath.Join(workspace, ".sprout"), MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{"shared": serverConfig("shared", "workspace-cmd")},
	})

	cfg, err := LoadMCPConfigForWorkspace(workspace)
	require.NoError(t, err)
	assert.Equal(t, "workspace-cmd", cfg.Servers["shared"].Command, "workspace layer must win the collision")
	assert.Equal(t, "global-only-cmd", cfg.Servers["global"].Command, "global-only servers must survive")
}

// A repo with no workspace MCP config still sees the globally-configured
// servers.
func TestLoadMCPConfigForWorkspace_GlobalVisibleWithoutWorkspaceLayer(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_ENABLED", "")
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	writeMCPLayer(t, globalDir, MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{"global": serverConfig("global", "global-cmd")},
	})

	cfg, err := LoadMCPConfigForWorkspace(workspace)
	require.NoError(t, err)
	assert.Contains(t, cfg.Servers, "global")
}

// $HOME is not a project: its .sprout would alias the user-level state
// directory, so a home workspace root contributes no workspace layer.
func TestMCPConfigCandidates_HomeRootHasNoWorkspaceLayer(t *testing.T) {
	globalDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("HOME", home)

	paths := mcpConfigCandidates(home)
	require.Len(t, paths, 1, "a home workspace root must not add a workspace layer")
	assert.Equal(t, filepath.Join(globalDir, mcpConfigFileName), paths[0])
}

// The reported source path names the workspace file when one exists, so a
// "no MCP servers" report can be traced to the config that was consulted.
func TestMCPConfigSourcePath_NamesWorkspaceFile(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)

	workspaceFile := filepath.Join(workspace, ".sprout", mcpConfigFileName)
	writeMCPLayer(t, filepath.Join(workspace, ".sprout"), MCPConfig{Enabled: true})

	assert.Equal(t, workspaceFile, MCPConfigSourcePath(workspace))
}

// With no workspace file the reported source is the global config, so the
// report still names a real path rather than nothing.
func TestMCPConfigSourcePath_FallsBackToGlobal(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	writeMCPLayer(t, globalDir, MCPConfig{Enabled: true})

	assert.Equal(t, filepath.Join(globalDir, mcpConfigFileName), MCPConfigSourcePath(workspace))
}

// A save scoped to a workspace writes the workspace layer, so `mcp add`
// inside a repo produces a file the agent serving that repo will read.
func TestSaveMCPConfigForWorkspace_WritesWorkspaceLayer(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	cfg := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{"added": serverConfig("added", "added-cmd")},
	}
	require.NoError(t, SaveMCPConfigForWorkspace(&cfg, workspace))

	workspaceFile := filepath.Join(workspace, ".sprout", mcpConfigFileName)
	_, err := os.Stat(workspaceFile)
	require.NoError(t, err, "save must write the workspace layer")

	// And a load for that workspace sees it.
	loaded, err := LoadMCPConfigForWorkspace(workspace)
	require.NoError(t, err)
	assert.Contains(t, loaded.Servers, "added")
}

// An explicit "enabled": false in the config file must survive the merge:
// the defaults start Enabled and AutoDiscover true, so OR-ing them would
// silently re-enable a feature the user turned off.
func TestLoadMCPConfigForWorkspace_ExplicitFalseSurvives(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", globalDir)
	t.Setenv("SPROUT_MCP_ENABLED", "")
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	writeMCPLayer(t, globalDir, MCPConfig{
		Enabled:      false,
		AutoDiscover: false,
		Servers:      map[string]MCPServerConfig{"s": serverConfig("s", "cmd")},
	})

	cfg, err := LoadMCPConfigForWorkspace("")
	require.NoError(t, err)
	assert.False(t, cfg.Enabled, "explicit enabled:false must not be overridden by the default")
	assert.False(t, cfg.AutoDiscover, "explicit auto_discover:false must not be overridden by the default")
}
