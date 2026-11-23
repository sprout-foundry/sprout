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

func TestProjectFolderConfigResolution(t *testing.T) {
	t.Run("prefers workspace.json when present", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "workspace.json"), `{}`)
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), GetProjectFolderConfigPath(root))
	})

	// Existing project folders must keep working with no migration.
	t.Run("falls back to a legacy config.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "config.json"), GetProjectFolderConfigPath(root))
		assert.True(t, IsProjectFolderConfigPresent(root))
	})

	t.Run("writes always target workspace.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), ProjectFolderConfigWritePath(root))
	})

	t.Run("empty workspace root yields empty paths", func(t *testing.T) {
		assert.Equal(t, "", GetProjectFolderConfigPath(""))
		assert.Equal(t, "", ProjectFolderConfigWritePath(""))
		assert.Equal(t, "", ProjectFolderConfigDir(""))
		assert.False(t, IsProjectFolderConfigPresent(""))
	})
}

// At $HOME the project folder layer and the user-level state directory are the same
// folder, so a legacy config.json there is the user's GLOBAL config. Reading it
// as a project folder layer aliases a global preference into a per-project-folder
// override — and every existing install has that file. $HOME resolves to no project folder layer at all.
func TestProjectFolderConfigNeverFallsBackToGlobalAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".sprout", "config.json"), `{"skip_prompt":true}`)

	assert.Equal(t, "", GetProjectFolderConfigPath(home),
		"home must not resolve its workspace layer to the user-level config.json")
	assert.False(t, IsProjectFolderConfigPresent(home))
}

// A directory merely named like home, or nested under it, is a normal project folder.
func TestProjectFolderConfigFallbackAppliesBelowHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := filepath.Join(home, "dev", "project")
	legacy := filepath.Join(project, ".sprout", "config.json")
	writeFile(t, legacy, `{}`)

	assert.Equal(t, legacy, GetProjectFolderConfigPath(project),
		"a subdirectory of home is an ordinary workspace and keeps its legacy config")
}

// The layered manager must persist to workspace.json, never back into the
// legacy file and never into the user's global config.json.
func TestLayeredManagerWritesProjectFolderFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	require.NoError(t, os.MkdirAll(globalDir, 0700))
	globalCfg := filepath.Join(globalDir, ConfigFileName)
	writeFile(t, globalCfg, `{"version":"2.0"}`)
	globalBefore, err := os.ReadFile(globalCfg)
	require.NoError(t, err)

	projectFolderDir := filepath.Join(home, "proj", ".sprout")
	require.NoError(t, os.MkdirAll(projectFolderDir, 0700))

	mgr, err := NewManagerWithLayers(globalDir, projectFolderDir)
	require.NoError(t, err)
	require.NoError(t, mgr.SaveConfig())

	_, err = os.Stat(filepath.Join(projectFolderDir, ProjectFolderConfigFileName))
	assert.NoError(t, err, "workspace layer should be saved as workspace.json")

	_, err = os.Stat(filepath.Join(projectFolderDir, ConfigFileName))
	assert.True(t, os.IsNotExist(err), "must not create a legacy config.json in the workspace")

	globalAfter, err := os.ReadFile(globalCfg)
	require.NoError(t, err)
	assert.Equal(t, string(globalBefore), string(globalAfter), "global config must be untouched")
}

// $HOME has no project folder layer at all — not for reads, and critically not for
// writes. NewManagerWithLayers uses the project folder dir as the SAVE target, so a
// daemon running with project folder=$HOME (what `sprout service install` produces)
// would otherwise write its full merged config to ~/.sprout/workspace.json on
// the first settings save, and read it back next start as a deliberate
// per-project-folder opt-in.
func TestHomeHasNoProjectFolderLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := ProjectFolderConfigDir(home); got != "" {
		t.Errorf("WorkspaceConfigDir($HOME) = %q, want \"\"", got)
	}
	if got := ProjectFolderConfigWritePath(home); got != "" {
		t.Errorf("WorkspaceConfigWritePath($HOME) = %q, want \"\"", got)
	}
	if got := GetProjectFolderConfigPath(home); got != "" {
		t.Errorf("GetWorkspaceConfigPath($HOME) = %q, want \"\"", got)
	}
	if IsProjectFolderConfigPresent(home) {
		t.Error("IsWorkspaceConfigPresent($HOME) = true, want false")
	}
}

// Even when a workspace.json already exists at $HOME — which is exactly the
// state a previous daemon left behind — it must not be picked up.
func TestHomeIgnoresPreexistingProjectFolderFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".sprout", ProjectFolderConfigFileName),
		`{"skip_prompt":true}`)

	if got := GetProjectFolderConfigPath(home); got != "" {
		t.Errorf("a machine-written workspace.json at $HOME must be ignored, got %q", got)
	}
	if IsProjectFolderConfigPresent(home) {
		t.Error("IsWorkspaceConfigPresent($HOME) = true despite no workspace layer")
	}
}

// A layered manager rooted at $HOME must save to the GLOBAL config, never
// create ~/.sprout/workspace.json.
func TestLayeredManagerAtHomeSavesGlobally(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	require.NoError(t, os.MkdirAll(globalDir, 0700))
	writeFile(t, filepath.Join(globalDir, ConfigFileName), `{"version":"2.0"}`)

	mgr, err := NewManagerWithLayers(globalDir, ProjectFolderConfigDir(home))
	require.NoError(t, err)
	require.NoError(t, mgr.SaveConfig())

	_, err = os.Stat(filepath.Join(home, ".sprout", ProjectFolderConfigFileName))
	assert.True(t, os.IsNotExist(err),
		"saving with workspace=$HOME must not create ~/.sprout/workspace.json")

	_, err = os.Stat(filepath.Join(globalDir, ConfigFileName))
	assert.NoError(t, err, "the save should have landed in the global config")
}
