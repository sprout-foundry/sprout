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
	t.Run("prefers project.json over the earlier names", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "project.json"), `{}`)
		writeFile(t, filepath.Join(root, ".sprout", "workspace.json"), `{}`)
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "project.json"), GetProjectFolderConfigPath(root))
		assert.Equal(t, filepath.Join(root, ".sprout", "project.json"), ProjectFolderConfigWritePath(root))
	})

	t.Run("reads and writes an existing workspace.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "workspace.json"), `{}`)
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), GetProjectFolderConfigPath(root))
		assert.Equal(t, filepath.Join(root, ".sprout", "workspace.json"), ProjectFolderConfigWritePath(root))
	})

	// Existing project folders must keep working with no migration.
	t.Run("falls back to a legacy config.json", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)

		assert.Equal(t, filepath.Join(root, ".sprout", "config.json"), GetProjectFolderConfigPath(root))
		assert.True(t, IsProjectFolderConfigPresent(root))
	})

	t.Run("a new file is project.json, never config.json", func(t *testing.T) {
		root := t.TempDir()
		assert.Equal(t, filepath.Join(root, ".sprout", "project.json"), ProjectFolderConfigWritePath(root))
		writeFile(t, filepath.Join(root, ".sprout", "config.json"), `{}`)
		assert.Equal(t, filepath.Join(root, ".sprout", "project.json"), ProjectFolderConfigWritePath(root))
	})

	t.Run("local overrides prefer project.local.json", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), ".sprout")
		assert.Equal(t, filepath.Join(dir, "project.local.json"), ProjectFolderLocalFile(dir))
		writeFile(t, filepath.Join(dir, "workspace.local.json"), `{}`)
		assert.Equal(t, filepath.Join(dir, "workspace.local.json"), ProjectFolderLocalFile(dir))
		writeFile(t, filepath.Join(dir, "project.local.json"), `{}`)
		assert.Equal(t, filepath.Join(dir, "project.local.json"), ProjectFolderLocalFile(dir))
	})

	t.Run("empty project folder yields empty paths", func(t *testing.T) {
		assert.Equal(t, "", GetProjectFolderConfigPath(""))
		assert.Equal(t, "", ProjectFolderConfigWritePath(""))
		assert.Equal(t, "", ProjectFolderConfigDir(""))
		assert.False(t, IsProjectFolderConfigPresent(""))
	})
}

// A project folder that already has workspace.json keeps saving to it rather
// than starting a second file beside it.
func TestLayeredManagerKeepsAnExistingWorkspaceFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName), `{"version":"2.0"}`)
	projectFolderDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(projectFolderDir, LegacyProjectFolderConfigFileName), `{}`)

	mgr, err := NewManagerWithLayers(globalDir, projectFolderDir)
	require.NoError(t, err)
	require.NoError(t, mgr.SaveConfig())

	_, err = os.Stat(filepath.Join(projectFolderDir, ProjectFolderConfigFileName))
	assert.True(t, os.IsNotExist(err), "must not create project.json beside an existing workspace.json")
	require.NoError(t, mgr.Reload())
	require.NoError(t, mgr.SaveConfig())
	_, err = os.Stat(filepath.Join(projectFolderDir, ProjectFolderConfigFileName))
	assert.True(t, os.IsNotExist(err), "a reload must keep the workspace.json save target")
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
		"home must not resolve its project folder layer to the user-level config.json")
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
		"a subdirectory of home is an ordinary project folder and keeps its legacy config")
}

// The layered manager must persist to project.json, never back into the
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
	assert.NoError(t, err, "project folder layer should be saved as project.json")

	_, err = os.Stat(filepath.Join(projectFolderDir, ConfigFileName))
	assert.True(t, os.IsNotExist(err), "must not create a legacy config.json in the project folder")

	globalAfter, err := os.ReadFile(globalCfg)
	require.NoError(t, err)
	assert.Equal(t, string(globalBefore), string(globalAfter), "global config must be untouched")
}

// $HOME has no project folder layer at all — not for reads, and critically not for
// writes. NewManagerWithLayers uses the project folder dir as the SAVE target, so a
// daemon running with project folder=$HOME (what `sprout service install` produces)
// would otherwise write its full merged config to ~/.sprout/project.json on
// the first settings save, and read it back next start as a deliberate
// per-project-folder opt-in.
func TestHomeHasNoProjectFolderLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := ProjectFolderConfigDir(home); got != "" {
		t.Errorf("ProjectFolderConfigDir($HOME) = %q, want \"\"", got)
	}
	if got := ProjectFolderConfigWritePath(home); got != "" {
		t.Errorf("ProjectFolderConfigWritePath($HOME) = %q, want \"\"", got)
	}
	if got := GetProjectFolderConfigPath(home); got != "" {
		t.Errorf("GetProjectFolderConfigPath($HOME) = %q, want \"\"", got)
	}
	if IsProjectFolderConfigPresent(home) {
		t.Error("IsProjectFolderConfigPresent($HOME) = true, want false")
	}
}

// Even when a project.json already exists at $HOME — which is exactly the
// state a previous daemon left behind — it must not be picked up.
func TestHomeIgnoresPreexistingProjectFolderFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".sprout", ProjectFolderConfigFileName),
		`{"skip_prompt":true}`)

	if got := GetProjectFolderConfigPath(home); got != "" {
		t.Errorf("a machine-written project.json at $HOME must be ignored, got %q", got)
	}
	if IsProjectFolderConfigPresent(home) {
		t.Error("IsProjectFolderConfigPresent($HOME) = true despite no project folder layer")
	}
}

// A layered manager rooted at $HOME must save to the GLOBAL config, never
// create ~/.sprout/project.json.
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
		"saving with the project folder at $HOME must not create ~/.sprout/project.json")

	_, err = os.Stat(filepath.Join(globalDir, ConfigFileName))
	assert.NoError(t, err, "the save should have landed in the global config")
}
