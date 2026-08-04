package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}

func TestWorkspaceConfigResolution(t *testing.T) {
	t.Run("prefers workspace.json when present", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "workspace.json"), `{}`)
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), GetWorkspaceConfigPath(root))
	})

	// Existing workspaces must keep working with no migration.
	t.Run("falls back to a legacy config.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "config.json"), GetWorkspaceConfigPath(root))
		assert.True(t, IsWorkspaceConfigPresent(root))
	})

	t.Run("writes always target workspace.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), WorkspaceConfigWritePath(root))
	})

	t.Run("empty workspace root yields empty paths", func(t *testing.T) {
		assert.Equal(t, "", GetWorkspaceConfigPath(""))
		assert.Equal(t, "", WorkspaceConfigWritePath(""))
		assert.Equal(t, "", WorkspaceConfigDir(""))
		assert.False(t, IsWorkspaceConfigPresent(""))
	})
}

// The whole point of the split: at $HOME the workspace layer and the user-level
// state directory are the same folder, so a legacy config.json there is the
// user's GLOBAL config. Falling back to it would re-create the aliasing that
// turned a global "embeddings on" preference into indexing the entire home
// directory — and every existing install has that file.
func TestWorkspaceConfigNeverFallsBackToGlobalAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".sprout", "config.json"), `{"embedding_index":{"enabled":true}}`)

	got := GetWorkspaceConfigPath(home)
	assert.Equal(t, filepath.Join(home, ".sprout", "workspace.json"), got,
		"home must not resolve its workspace layer to the user-level config.json")

	_, err := os.Stat(got)
	assert.True(t, os.IsNotExist(err), "there should be no workspace layer at home by default")
	assert.False(t, IsWorkspaceConfigPresent(home))
}

// A user who deliberately runs with $HOME as the workspace still gets a real
// workspace layer — it just has to be an explicit workspace.json.
func TestWorkspaceConfigHonorsExplicitHomeWorkspaceFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	explicit := filepath.Join(home, ".sprout", "workspace.json")
	writeFile(t, explicit, `{}`)

	assert.Equal(t, explicit, GetWorkspaceConfigPath(home))
	assert.True(t, IsWorkspaceConfigPresent(home))
}

// A directory merely named like home, or nested under it, is a normal workspace.
func TestWorkspaceConfigFallbackAppliesBelowHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := filepath.Join(home, "dev", "project")
	legacy := filepath.Join(project, ".sprout", "config.json")
	writeFile(t, legacy, `{}`)

	assert.Equal(t, legacy, GetWorkspaceConfigPath(project),
		"a subdirectory of home is an ordinary workspace and keeps its legacy config")
}

// The layered manager must persist to workspace.json, never back into the
// legacy file and never into the user's global config.json.
func TestLayeredManagerWritesWorkspaceFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	require.NoError(t, os.MkdirAll(globalDir, 0700))
	globalCfg := filepath.Join(globalDir, ConfigFileName)
	writeFile(t, globalCfg, `{"version":"2.0"}`)
	globalBefore, err := os.ReadFile(globalCfg)
	require.NoError(t, err)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	require.NoError(t, os.MkdirAll(workspaceDir, 0700))

	mgr, err := NewManagerWithLayers(globalDir, workspaceDir)
	require.NoError(t, err)
	require.NoError(t, mgr.SaveConfig())

	_, err = os.Stat(filepath.Join(workspaceDir, WorkspaceConfigFileName))
	assert.NoError(t, err, "workspace layer should be saved as workspace.json")

	_, err = os.Stat(filepath.Join(workspaceDir, ConfigFileName))
	assert.True(t, os.IsNotExist(err), "must not create a legacy config.json in the workspace")

	globalAfter, err := os.ReadFile(globalCfg)
	require.NoError(t, err)
	assert.Equal(t, string(globalBefore), string(globalAfter), "global config must be untouched")
}
