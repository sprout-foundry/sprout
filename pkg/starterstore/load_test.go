package starterstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// fixtureFull is a fully-populated, valid .sprout/starter.json document.
const fixtureFull = `{
  "starter": {"id": "web-app", "version": "1.2.0"},
  "build": "npm run build",
  "test": "npm test",
  "dev": "npm run dev",
  "preview": "npx serve dist",
  "dev_port": 5173,
  "routes": ["/", "/login", "/dashboard"],
  "build_output": "dist"
}`

// fixtureMinimal is the leanest valid manifest: just the starter identity.
// It proves the loader never invents commands for absent fields.
const fixtureMinimal = `{"starter": {"id": "static-site", "version": "0.1.0"}}`

// writeManifest creates .sprout/starter.json under root with content.
func writeManifest(t *testing.T, root, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(StarterManifestPath(root), []byte(content), 0o644))
}

func TestLoadMissingManifest(t *testing.T) {
	t.Run("no .sprout directory at all", func(t *testing.T) {
		root := t.TempDir()
		m, err := LoadStarterManifest(root)
		require.ErrorIs(t, err, ErrNoManifest, "a missing .sprout dir must surface the ErrNoManifest sentinel")
		assert.Nil(t, m, "no manifest may be returned when the file is absent")
	})

	t.Run(".sprout exists but starter.json does not", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))

		m, err := LoadStarterManifest(root)
		require.ErrorIs(t, err, ErrNoManifest, "a missing file must surface the ErrNoManifest sentinel")
		assert.Nil(t, m)
	})
}

func TestLoadValidManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, fixtureFull)

	m, err := LoadStarterManifest(root)
	require.NoError(t, err, "a valid manifest on disk must load")
	require.NotNil(t, m)

	assert.Equal(t, "web-app", m.Starter.ID)
	assert.Equal(t, "1.2.0", m.Starter.Version)
	assert.Equal(t, "npm run build", m.Build)
	assert.Equal(t, "npm test", m.Test)
	assert.Equal(t, "npm run dev", m.Dev)
	assert.Equal(t, "npx serve dist", m.Preview)
	assert.Equal(t, 5173, m.DevPort)
	assert.Equal(t, []string{"/", "/login", "/dashboard"}, m.Routes)
	assert.Equal(t, "dist", m.BuildOutput)
}

// TestLoadMinimalManifestNeverGuessed pins the "never guessed" contract:
// a valid-but-minimal manifest (starter identity only) loads
// with every command field empty — the loader did not invent commands, ports,
// routes, or a build output for the absent fields.
func TestLoadMinimalManifestNeverGuessed(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, fixtureMinimal)

	m, err := LoadStarterManifest(root)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.Equal(t, "static-site", m.Starter.ID)
	assert.Equal(t, "0.1.0", m.Starter.Version)
	assert.Empty(t, m.Build, "the loader must not invent a build command")
	assert.Empty(t, m.Test, "the loader must not invent a test command")
	assert.Empty(t, m.Dev, "the loader must not invent a dev command")
	assert.Empty(t, m.Preview, "the loader must not invent a preview command")
	assert.Zero(t, m.DevPort, "the loader must not invent a dev port")
	assert.Empty(t, m.Routes, "the loader must not invent routes")
	assert.Empty(t, m.BuildOutput, "the loader must not invent a build output directory")
}

func TestLoadCorruptJSON(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "{ not valid json ")

	m, err := LoadStarterManifest(root)
	require.Error(t, err, "corrupt JSON must not load")
	assert.Nil(t, m, "a corrupt file must not return a manifest")
	assert.False(t, errors.Is(err, ErrNoManifest), "corrupt is a different error than missing")
	assert.Contains(t, err.Error(), "invalid starter JSON", "the decode failure must be surfaced: %v", err)
}

// TestLoadInvalidManifest pins that a well-formed JSON document that fails
// validation is a hard error (not ErrNoManifest, not a guessed manifest),
// and that the structured *startermanifest.ValidationError is wrapped so
// callers can recover it with errors.As.
func TestLoadInvalidManifest(t *testing.T) {
	root := t.TempDir()
	// Valid JSON, missing the required starter version.
	writeManifest(t, root, `{"starter": {"id": "web-app"}, "build": "npm run build"}`)

	m, err := LoadStarterManifest(root)
	require.Error(t, err, "an invalid manifest must not load")
	assert.Nil(t, m, "an invalid manifest must not be returned")
	assert.False(t, errors.Is(err, ErrNoManifest), "invalid is a different error than missing")

	var vErr *startermanifest.ValidationError
	require.ErrorAs(t, err, &vErr, "the error must wrap the *startermanifest.ValidationError")
	assert.Contains(t, err.Error(), "starter.version is required", "got: %v", err)
}

// TestLoadPresentButUnreadable pins the "present but broken" path: the file
// exists (here as a directory, so the read fails), so the result is a read
// error, never ErrNoManifest and never a guessed manifest.
func TestLoadPresentButUnreadable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(StarterManifestPath(root), 0o755))

	m, err := LoadStarterManifest(root)
	require.Error(t, err, "a present-but-unreadable manifest must surface an error")
	assert.Nil(t, m)
	assert.False(t, errors.Is(err, ErrNoManifest), "an unreadable file is not a missing one")
}

func TestStarterManifestPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/proj", ".sprout", "starter.json"), StarterManifestPath("/proj"))
}
