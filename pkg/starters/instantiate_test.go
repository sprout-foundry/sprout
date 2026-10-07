package starters

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// fixtureTreeFiles are the fixture-tree files (relative to the destination)
// an instantiation must produce: every file of the embedded tree except
// the top-level descriptor, which becomes .sprout/starter.json instead.
var fixtureTreeFiles = []string{
	"README.md",
	"index.html",
	filepath.Join("src", "main.js"),
}

// TestInstantiateIntoNonexistentNestedPath covers the mkdirs path: a
// destination that does not exist (with missing parents) is created and
// populated, the copied content matches the embedded tree byte for byte,
// and the written manifest loads back through the existing loader.
func TestInstantiateIntoNonexistentNestedPath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "a", "b", "proj")

	require.NoError(t, Instantiate("fixture", dest))

	for _, rel := range fixtureTreeFiles {
		data, err := os.ReadFile(filepath.Join(dest, rel))
		require.NoErrorf(t, err, "missing %s in the instantiated project", rel)
		want, err := fs.ReadFile(dataFS, dataRoot+"/fixture/"+rel)
		require.NoError(t, err)
		assert.Equal(t, want, data, "copied content must match the embedded tree (%s)", rel)
	}

	// The descriptor is never copied verbatim: it becomes the manifest.
	_, err := os.Stat(filepath.Join(dest, DescriptorName))
	assert.True(t, os.IsNotExist(err), "the top-level descriptor must not be copied into the project")

	// The written manifest must load back cleanly through the existing
	// loader with the fixture's id, version, and commands.
	m, err := starterstore.LoadStarterManifest(dest)
	require.NoError(t, err, "the written manifest must load through pkg/starterstore")
	assert.Equal(t, "fixture", m.Starter.ID)
	assert.Equal(t, fixtureVersion, m.Starter.Version)
	assert.Equal(t, "npm run build", m.Build)
	assert.Equal(t, "npm test", m.Test)
	assert.Equal(t, "npm run dev", m.Dev)
	assert.Equal(t, 3000, m.DevPort)
	assert.Equal(t, []string{"/"}, m.Routes)
	assert.Equal(t, "dist", m.BuildOutput)

	_, err = os.Stat(starterstore.StarterManifestPath(dest))
	require.NoError(t, err, ".sprout/starter.json must exist")
}

// TestInstantiateIntoExistingEmptyDirectory covers the "exists and is
// empty" case of the destination contract: an existing empty directory is
// accepted.
func TestInstantiateIntoExistingEmptyDirectory(t *testing.T) {
	dest := t.TempDir() // exists, empty

	require.NoError(t, Instantiate("fixture", dest))

	m, err := starterstore.LoadStarterManifest(dest)
	require.NoError(t, err)
	assert.Equal(t, "fixture", m.Starter.ID)
}

// TestInstantiateRefusesNonEmptyDestination pins the core safety contract:
// a destination that contains anything is refused with
// ErrNonEmptyDestination, nothing is written, and the existing content is
// untouched.
func TestInstantiateRefusesNonEmptyDestination(t *testing.T) {
	t.Run("existing file", func(t *testing.T) {
		dest := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("precious"), 0o644))
		before := walkFiles(t, dest)

		err := Instantiate("fixture", dest)
		require.ErrorIs(t, err, ErrNonEmptyDestination)

		assert.Equal(t, before, walkFiles(t, dest), "a refused instantiation must not add or remove files")
		data, err := os.ReadFile(filepath.Join(dest, "keep.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("precious"), data, "existing content must be untouched")

		_, err = os.Stat(starterstore.StarterManifestPath(dest))
		assert.True(t, os.IsNotExist(err), "no manifest may be written on refusal")
	})

	t.Run("existing subdirectory", func(t *testing.T) {
		dest := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dest, "sub"), 0o755))

		err := Instantiate("fixture", dest)
		require.ErrorIs(t, err, ErrNonEmptyDestination)
	})
}

// TestInstantiateDestinationIsAFile covers the third case of the
// destination contract: a destination that exists as a file gets a clear
// error that is not the non-empty sentinel.
func TestInstantiateDestinationIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	err := Instantiate("fixture", file)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNonEmptyDestination, "a file destination is a different, clearer error")
	assert.Contains(t, err.Error(), "not a directory")
}

func TestInstantiateEmptyDestinationPath(t *testing.T) {
	require.Error(t, Instantiate("fixture", ""))
}

// TestInstantiateUnknownStarterWritesNothing pins the ordering of
// Instantiate: the starter is resolved before the destination is touched,
// so an unknown starter creates no directory at all.
func TestInstantiateUnknownStarterWritesNothing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "proj")

	err := Instantiate("no-such-starter", dest)
	require.ErrorIs(t, err, ErrUnknownStarter)

	_, statErr := os.Stat(dest)
	assert.True(t, os.IsNotExist(statErr), "no destination may be created for an unknown starter")
}

// TestInstantiateTwiceIntoTwoDirectories proves the mechanism is repeatable
// for the same starter: two independent instantiations both produce a
// valid project.
func TestInstantiateTwiceIntoTwoDirectories(t *testing.T) {
	for _, suffix := range []string{"one", "two"} {
		dest := filepath.Join(t.TempDir(), suffix)
		require.NoErrorf(t, Instantiate("fixture", dest), "dest %s", suffix)

		m, err := starterstore.LoadStarterManifest(dest)
		require.NoError(t, err)
		assert.Equal(t, "fixture", m.Starter.ID)
		assert.Equal(t, fixtureVersion, m.Starter.Version)

		_, err = os.ReadFile(filepath.Join(dest, "index.html"))
		assert.NoErrorf(t, err, "dest %s must carry the fixture tree", suffix)
	}
}

// walkFiles lists every regular file under root as sorted relative paths.
func walkFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, rel)
		}
		return nil
	})
	require.NoError(t, err)
	return out
}
