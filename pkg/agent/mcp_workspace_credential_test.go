package agent

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent must register its workspace's credential store as the MCP
// fallback scope during initialization. That is what lets a daemon — whose
// own SPROUT_CONFIG points at the global store — authenticate a platform MCP
// server whose token a workspace-scoped `sprout login` stored in the
// workspace's store.
func TestInitializeMCP_RegistersWorkspaceCredentialDir(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")

	t.Cleanup(func() { mcp.SetWorkspaceCredentialDir("") })

	agent := newTestAgent(t)
	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	require.NoError(t, agent.initializeMCP())

	assert.Equal(t, filepath.Join(workspace, ".sprout"), mcp.GetWorkspaceCredentialDir(),
		"the workspace config dir must become the MCP credential fallback scope")
}

// A $HOME workspace has no separate .sprout layer — the fallback must not be
// registered, since that store is the global one the process already reads.
func TestInitializeMCP_HomeWorkspaceSkipsCredentialDir(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")

	prev := mcp.GetWorkspaceCredentialDir()
	t.Cleanup(func() { mcp.SetWorkspaceCredentialDir(prev) })
	mcp.SetWorkspaceCredentialDir("/nonexistent/previous")

	agent := newTestAgent(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent.SetWorkspaceRoot(home)

	require.NoError(t, agent.initializeMCP())

	assert.Equal(t, "/nonexistent/previous", mcp.GetWorkspaceCredentialDir(),
		"a $HOME workspace shares the global store; the fallback must stay untouched")
}
