//go:build !js

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/starters"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// TestRunNewProject_InstallsFixture drives the happy path: the fixture
// starter is instantiated into a fresh directory, the manifest loads back
// through the existing loader with the fixture id and version, and a short
// confirmation naming the destination, starter, and version is printed.
func TestRunNewProject_InstallsFixture(t *testing.T) {
	dest := t.TempDir()
	var out bytes.Buffer

	err := runNewProject("fixture", dest, &out)
	require.NoError(t, err)

	// The written manifest loads back through the existing loader.
	m, err := starterstore.LoadStarterManifest(dest)
	require.NoError(t, err, "the written manifest must load through pkg/starterstore")
	assert.Equal(t, "fixture", m.Starter.ID)
	assert.Equal(t, "0.1.0", m.Starter.Version)

	// Every fixture-tree file (except the descriptor) is present.
	for _, rel := range []string{"README.md", "index.html", filepath.Join("src", "main.js")} {
		_, err := os.Stat(filepath.Join(dest, rel))
		assert.NoErrorf(t, err, "expected %s in the instantiated project", rel)
	}

	// The confirmation names the destination, starter id, and version.
	got := out.String()
	assert.Contains(t, got, "fixture")
	assert.Contains(t, got, "0.1.0")
	assert.Contains(t, got, dest)
}

// TestRunNewProject_UnknownStarterListsAvailable pins the "listing available
// starters on unknown ID" requirement (153.4): an unknown id prints the
// user-facing catalogue (test-only starters such as the fixture are
// withheld) and returns an error, and writes nothing to the destination.
func TestRunNewProject_UnknownStarterListsAvailable(t *testing.T) {
	dest := t.TempDir()
	var out bytes.Buffer

	err := runNewProject("does-not-exist", dest, &out)
	require.Error(t, err)

	got := out.String()
	assert.Contains(t, got, "Available starters")
	// The test-only fixture is withheld from the chooser (fix.9).
	assert.NotContains(t, got, "fixture")

	// The starter is resolved before the destination is touched, so nothing
	// is written for an unknown id.
	_, statErr := os.Stat(starterstore.StarterManifestPath(dest))
	assert.True(t, os.IsNotExist(statErr), "no manifest may be written for an unknown starter")
}

// TestRunNewProject_EmptyStarterListsAvailable covers the no-starter path:
// the user-facing catalogue is printed (test-only starters withheld) and an
// error is returned; a project cannot be created without naming a starter
// (153.4).
func TestRunNewProject_EmptyStarterListsAvailable(t *testing.T) {
	dest := t.TempDir()
	var out bytes.Buffer

	err := runNewProject("", dest, &out)
	require.Error(t, err)

	got := out.String()
	assert.Contains(t, got, "Available starters")
	// The test-only fixture is withheld from the chooser (fix.9).
	assert.NotContains(t, got, "fixture")

	_, statErr := os.Stat(starterstore.StarterManifestPath(dest))
	assert.True(t, os.IsNotExist(statErr), "no manifest may be written when no starter is named")
}

// TestRunNewProject_NonEmptyDestinationRefused pins the safety contract: a
// destination that already contains a file is refused with
// starters.ErrNonEmptyDestination, the existing content is untouched, no
// manifest is written, and no confirmation is printed.
func TestRunNewProject_NonEmptyDestinationRefused(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("precious"), 0o644))
	var out bytes.Buffer

	err := runNewProject("fixture", dest, &out)
	require.Error(t, err)
	assert.ErrorIs(t, err, starters.ErrNonEmptyDestination)

	data, err := os.ReadFile(filepath.Join(dest, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("precious"), data, "existing content must be untouched")

	_, statErr := os.Stat(starterstore.StarterManifestPath(dest))
	assert.True(t, os.IsNotExist(statErr), "no manifest may be written on refusal")

	assert.Empty(t, out.String(), "a refused instantiation must not print a confirmation")
}

// TestRunNewProject_DefaultDirNamedAfterStarter covers the omitted-directory
// default: with no directory given, the project is created in a directory
// named after the starter id (here the relative "fixture").
func TestRunNewProject_DefaultDirNamedAfterStarter(t *testing.T) {
	// Isolate the relative default in a throwaway working directory.
	// (t.Chdir self-fails the test and auto-restores the cwd on cleanup.)
	t.Chdir(t.TempDir())
	var out bytes.Buffer

	err := runNewProject("fixture", "", &out)
	require.NoError(t, err)

	m, err := starterstore.LoadStarterManifest("fixture") // ./fixture
	require.NoError(t, err, "the default destination ./fixture must carry the manifest")
	assert.Equal(t, "fixture", m.Starter.ID)
	assert.Contains(t, out.String(), "fixture")
}

// TestNewCmd_CobraFlagWiring drives the actual cobra command to confirm the
// --starter flag value flows into the body and the default destination is
// used when no positional directory is given.
func TestNewCmd_CobraFlagWiring(t *testing.T) {
	t.Chdir(t.TempDir())
	buf := new(bytes.Buffer)
	newCmd.SetOut(buf)

	// Simulate `sprout new --starter fixture` (no positional → default dir).
	// The flag is bound to the package-level newStarterID, so reset it on
	// teardown to keep the shared command instance clean for other tests.
	require.NoError(t, newCmd.Flags().Set("starter", "fixture"))
	t.Cleanup(func() { _ = newCmd.Flags().Set("starter", "") })

	err := newCmd.RunE(newCmd, []string{})
	require.NoError(t, err)

	m, err := starterstore.LoadStarterManifest("fixture")
	require.NoError(t, err, "./fixture must be instantiated by the cobra command")
	assert.Equal(t, "fixture", m.Starter.ID)
	assert.Contains(t, buf.String(), "fixture")
}
