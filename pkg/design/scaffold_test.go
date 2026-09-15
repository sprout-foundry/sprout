package design

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scaffoldPaths(t *testing.T, root string) (designDir, manifest string) {
	t.Helper()
	designDir = filepath.Join(root, DirName)
	manifest = filepath.Join(designDir, ManifestName)
	return designDir, manifest
}

func TestScaffoldCreatesFullTree(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	designDir, manifest := scaffoldPaths(t, root)
	info, err := os.Stat(designDir)
	require.NoError(t, err, "design/ root missing after Scaffold")
	assert.True(t, info.IsDir(), "design/ is not a directory")

	for _, sub := range Subdirs {
		t.Run(sub, func(t *testing.T) {
			subInfo, err := os.Stat(filepath.Join(designDir, sub))
			require.NoError(t, err, "subdirectory missing after Scaffold")
			assert.True(t, subInfo.IsDir(), "%s is not a directory", sub)

			gitkeepInfo, err := os.Stat(filepath.Join(designDir, sub, ".gitkeep"))
			require.NoError(t, err, ".gitkeep missing after Scaffold")
			assert.False(t, gitkeepInfo.IsDir(), ".gitkeep is a directory")
		})
	}

	manifestInfo, err := os.Stat(manifest)
	require.NoError(t, err, "design/README.md missing after Scaffold")
	assert.False(t, manifestInfo.IsDir())
	assert.NotEmpty(t, readFile(t, manifest), "manifest must be non-empty")
}

func TestScaffoldManifestMatchesTemplate(t *testing.T) {
	root := t.TempDir()

	template, err := ManifestTemplate()
	require.NoError(t, err)
	require.NotEmpty(t, template, "embedded manifest template must be non-empty")

	require.NoError(t, Scaffold(root))

	_, manifest := scaffoldPaths(t, root)
	assert.Equal(t, string(template), readFile(t, manifest),
		"on-disk manifest must equal the embedded template bytes")
}

func TestScaffoldManifestRoundTripsFrames(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	_, manifest := scaffoldPaths(t, root)
	frames, err := ParseFrames(readFile(t, manifest))
	require.NoError(t, err, "ParseFrames must accept the scaffolded manifest")
	assert.Equal(t, []Frame{
		{Name: "desktop", Width: 1440, Height: 900},
		{Name: "mobile", Width: 390, Height: 844},
		{Name: "tablet", Width: 768, Height: 1024},
	}, frames, "scaffolded manifest must yield the contract frames in order")
}

func TestScaffoldReplacesEmptyManifestPlaceholder(t *testing.T) {
	root := t.TempDir()

	template, err := ManifestTemplate()
	require.NoError(t, err)
	require.NotEmpty(t, template, "embedded manifest template must be non-empty")

	designDir, manifest := scaffoldPaths(t, root)
	require.NoError(t, os.MkdirAll(designDir, 0o755))
	require.NoError(t, os.WriteFile(manifest, nil, 0o644),
		"seeding a 0-byte manifest placeholder must not fail")

	require.NoError(t, Scaffold(root))

	assert.Equal(t, string(template), readFile(t, manifest),
		"0-byte placeholder manifest must be replaced by the template")
}

func TestScaffoldManifestPathIsDirectory(t *testing.T) {
	root := t.TempDir()

	designDir, manifest := scaffoldPaths(t, root)
	require.NoError(t, os.MkdirAll(filepath.Join(designDir, ManifestName), 0o755),
		"seeding a directory at the manifest path must not fail")

	err := Scaffold(root)
	require.Error(t, err, "Scaffold must fail when the manifest path is a directory")
	assert.True(t, errors.Is(err, ErrManifestIsDirectory),
		"error must wrap ErrManifestIsDirectory, got: %v", err)
	assert.DirExists(t, manifest, "directory at the manifest path must be left untouched")
	assert.NoFileExists(t, manifest, "no file may be written over the directory")

	for _, sub := range Subdirs {
		assert.DirExists(t, filepath.Join(root, DirName, sub),
			"subdirectory creation must survive the failed Scaffold")
		assert.FileExists(t, filepath.Join(root, DirName, sub, ".gitkeep"),
			".gitkeep creation must survive the failed Scaffold")
	}
}

func TestScaffoldTwiceRefusesToClobberManifest(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	_, manifest := scaffoldPaths(t, root)
	custom := "CUSTOM"
	require.NoError(t, os.WriteFile(manifest, []byte(custom), 0o644),
		"seeding a custom manifest must not fail")

	err := Scaffold(root)
	require.Error(t, err, "second Scaffold over an existing manifest must fail")
	assert.True(t, errors.Is(err, ErrManifestExists),
		"error must wrap ErrManifestExists, got: %v", err)
	assert.Equal(t, custom, readFile(t, manifest),
		"failed second Scaffold must not touch the existing manifest")

	for _, sub := range Subdirs {
		assert.DirExists(t, filepath.Join(root, DirName, sub),
			"subdirectory must survive the failed second Scaffold")
		assert.FileExists(t, filepath.Join(root, DirName, sub, ".gitkeep"),
			".gitkeep must survive the failed second Scaffold")
	}
}

func TestScaffoldKeepsExistingSubdirContent(t *testing.T) {
	root := t.TempDir()
	tokensDir := filepath.Join(root, DirName, "tokens")
	require.NoError(t, os.MkdirAll(tokensDir, 0o755))
	tokensFile := filepath.Join(tokensDir, "color.tokens.json")
	const tokensBody = `{"color": {"$type": "color", "$value": "#000000"}}`
	require.NoError(t, os.WriteFile(tokensFile, []byte(tokensBody), 0o644))

	require.NoError(t, Scaffold(root))

	assert.Equal(t, tokensBody, readFile(t, tokensFile),
		"Scaffold must not delete or alter pre-existing user content")

	for _, sub := range Subdirs {
		assert.FileExists(t, filepath.Join(root, DirName, sub, ".gitkeep"),
			".gitkeep must be created even in pre-existing subdirectories")
	}

	_, manifest := scaffoldPaths(t, root)
	assert.FileExists(t, manifest, "manifest must still be scaffolded")
}

func TestScaffoldNestedProjectRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project", "src")

	require.NoError(t, Scaffold(root))

	designDir, manifest := scaffoldPaths(t, root)
	assert.DirExists(t, designDir)
	assert.FileExists(t, manifest)
	for _, sub := range Subdirs {
		assert.DirExists(t, filepath.Join(designDir, sub))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
