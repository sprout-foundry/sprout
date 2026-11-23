package agent

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent must register its project folder's credential store as the MCP
// fallback scope during initialization. That is what lets a daemon — whose
// own SPROUT_CONFIG points at the global store — authenticate a platform MCP
// server whose token a project-folder-scoped `sprout login` stored in the
// project folder's store.
func TestInitializeMCP_RegistersProjectFolderCredentialDir(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")

	t.Cleanup(func() { mcp.SetProjectFolderCredentialDir("") })

	agent := newTestAgent(t)
	projectFolder := t.TempDir()
	agent.SetProjectFolder(projectFolder)

	require.NoError(t, agent.initializeMCP())

	assert.Equal(t, filepath.Join(projectFolder, ".sprout"), mcp.GetProjectFolderCredentialDir(),
		"the workspace config dir must become the MCP credential fallback scope")
}

// A $HOME project folder has no separate .sprout layer — the fallback must not be
// registered, since that store is the global one the process already reads.
func TestInitializeMCP_HomeProjectFolderSkipsCredentialDir(t *testing.T) {
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")

	prev := mcp.GetProjectFolderCredentialDir()
	t.Cleanup(func() { mcp.SetProjectFolderCredentialDir(prev) })
	mcp.SetProjectFolderCredentialDir("/nonexistent/previous")

	agent := newTestAgent(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent.SetProjectFolder(home)

	require.NoError(t, agent.initializeMCP())

	assert.Equal(t, "/nonexistent/previous", mcp.GetProjectFolderCredentialDir(),
		"a $HOME workspace shares the global store; the fallback must stay untouched")
}
