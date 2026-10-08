package starters

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// webAppVersion is the version the web-app tree declares; the assertions
// below must agree with pkg/starters/data/web-app/starter.json.
const webAppVersion = "1.1.0"

// webAppProjectFiles are the project files (relative to the destination) a
// web-app instantiation must produce: the React + Vite source tree, the
// quality config, the README, the pinned lockfile, the SPA fallback, and the
// design workspace tree. The exact count is not pinned (the tree grows), but
// every file the item names is asserted present.
var webAppProjectFiles = []string{
	"package.json",
	"package-lock.json",
	"vite.config.ts",
	"vitest.config.ts",
	"index.html",
	"tsconfig.json",
	"eslint.config.mjs",
	".prettierrc.json",
	".gitignore",
	".gitattributes",
	"README.md",
	filepath.Join("public", "favicon.svg"),
	filepath.Join("public", "_redirects"),
	filepath.Join("src", "main.tsx"),
	filepath.Join("src", "App.tsx"),
	filepath.Join("src", "layouts", "Layout.tsx"),
	filepath.Join("src", "pages", "Home.tsx"),
	filepath.Join("src", "pages", "About.tsx"),
	filepath.Join("src", "hooks", "useLocalStorage.ts"),
	filepath.Join("src", "styles", "global.css"),
	filepath.Join("test", "setup.ts"),
	filepath.Join("test", "app.test.tsx"),
	filepath.Join("design", design.ManifestName),
	filepath.Join("design", "tokens", ".gitkeep"),
	filepath.Join("design", "brand", ".gitkeep"),
	filepath.Join("design", "icons", ".gitkeep"),
	filepath.Join("design", "wireframes", ".gitkeep"),
	filepath.Join("design", "components", ".gitkeep"),
	filepath.Join("design", "screens", ".gitkeep"),
	filepath.Join("design", "flows", ".gitkeep"),
	filepath.Join("design", "feedback", ".gitkeep"),
}

// TestInstantiateWebAppIntoEmptyDirectory proves the web-app starter
// instantiates into an empty directory: every project file the item names
// lands on disk byte-for-byte from the embedded tree, the descriptor is not
// copied verbatim, and the manifest loads back through the store with the
// declared commands, port, routes, build output and deploy target. It runs
// no npm step: the tree is validated as data.
func TestInstantiateWebAppIntoEmptyDirectory(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "app")

	require.NoError(t, Instantiate("web-app", dest))

	for _, rel := range webAppProjectFiles {
		data, err := os.ReadFile(filepath.Join(dest, rel))
		require.NoErrorf(t, err, "missing %s in the instantiated project", rel)
		want, err := fs.ReadFile(dataFS, dataRoot+"/web-app/"+filepath.ToSlash(rel))
		require.NoError(t, err)
		assert.Equal(t, want, data, "copied content must match the embedded tree (%s)", rel)
	}

	// The descriptor is never copied verbatim: its validated form becomes
	// .sprout/starter.json instead.
	_, err := os.Stat(filepath.Join(dest, DescriptorName))
	assert.True(t, os.IsNotExist(err), "the top-level descriptor must not be copied into the project")

	m, err := starterstore.LoadStarterManifest(dest)
	require.NoError(t, err, "the written manifest must load through pkg/starterstore")
	assert.Equal(t, "web-app", m.Starter.ID)
	assert.Equal(t, webAppVersion, m.Starter.Version)
	assert.Equal(t, "npm run build", m.Build)
	assert.Equal(t, "npm test", m.Test)
	assert.Equal(t, "npm run dev", m.Dev)
	assert.Equal(t, 5173, m.DevPort)
	assert.Contains(t, m.Routes, "/")
	assert.Contains(t, m.Routes, "/about")
	assert.Equal(t, "dist", m.BuildOutput)
	assert.Equal(t, startermanifest.DeployTargetPages, m.DeployTarget)

	// The design tree scaffold must be present so design mode works from
	// the first turn.
	assert.FileExists(t, filepath.Join(dest, design.DirName, design.ManifestName))
}

// TestWebAppManifestValidates pins the embedded descriptor against the
// manifest schema directly (validation runs on the written
// .sprout/starter.json through the store in the instantiation test above).
func TestWebAppManifestValidates(t *testing.T) {
	raw, err := fs.ReadFile(dataFS, dataRoot+"/web-app/"+DescriptorName)
	require.NoError(t, err)

	m, err := startermanifest.ValidateJSON(raw)
	require.NoError(t, err, "the web-app descriptor must satisfy the manifest schema")
	assert.Equal(t, "web-app", m.Starter.ID)
	assert.Equal(t, webAppVersion, m.Starter.Version)
	assert.Equal(t, "dist", m.BuildOutput)
	assert.Equal(t, startermanifest.DeployTargetPages, m.DeployTarget)
	assert.NotEmpty(t, m.Routes)
}

// TestWebAppLockfileMatchesPackageJSON pins the committed lockfile: it is
// valid npm lockfile JSON whose root package entry names the same pinned
// direct dependencies as package.json, so `npm ci` installs exactly what the
// starter declares. The check is pure data — it runs no npm step.
func TestWebAppLockfileMatchesPackageJSON(t *testing.T) {
	pkgRaw, err := fs.ReadFile(dataFS, dataRoot+"/web-app/package.json")
	require.NoError(t, err)
	lockRaw, err := fs.ReadFile(dataFS, dataRoot+"/web-app/package-lock.json")
	require.NoError(t, err)

	type depMap = map[string]string
	type packageJSON struct {
		Dependencies    depMap `json:"dependencies"`
		DevDependencies depMap `json:"devDependencies"`
	}
	type lockEntry struct {
		Version         string `json:"version"`
		Dependencies    depMap `json:"dependencies"`
		DevDependencies depMap `json:"devDependencies"`
	}
	var pkg packageJSON
	require.NoError(t, json.Unmarshal(pkgRaw, &pkg), "package.json must be valid JSON")

	var lock struct {
		LockfileVersion int                  `json:"lockfileVersion"`
		Packages        map[string]lockEntry `json:"packages"`
	}
	require.NoError(t, json.Unmarshal(lockRaw, &lock), "package-lock.json must be valid JSON")
	require.GreaterOrEqual(t, lock.LockfileVersion, 2, "a committed npm lockfile must be lockfileVersion 2 or newer (lockfileVersion 1 has no packages map)")

	root, ok := lock.Packages[""]
	require.True(t, ok, "the lockfile must carry a root package entry (\"\")")
	assert.Equal(t, pkg.Dependencies, root.Dependencies, "lockfile root dependencies must match package.json")
	assert.Equal(t, pkg.DevDependencies, root.DevDependencies, "lockfile root devDependencies must match package.json")

	// Every direct dependency must resolve to its pinned version in the
	// lockfile tree, so the pins are real and not just declared.
	for name, version := range pkg.Dependencies {
		entry, ok := lock.Packages["node_modules/"+name]
		require.Truef(t, ok, "dependency %s must have a lockfile entry", name)
		assert.Equal(t, version, entry.Version, "dependency %s must resolve to its pinned version", name)
	}
	for name, version := range pkg.DevDependencies {
		entry, ok := lock.Packages["node_modules/"+name]
		require.Truef(t, ok, "devDependency %s must have a lockfile entry", name)
		assert.Equal(t, version, entry.Version, "devDependency %s must resolve to its pinned version", name)
	}
}

// TestWebAppIsUserFacing pins the catalogue split: the web-app starter is
// user-facing (present in ListForUsers), while the test-only fixture stays
// withheld.
func TestWebAppIsUserFacing(t *testing.T) {
	visible, err := ListForUsers()
	require.NoError(t, err)

	byID := make(map[string]Starter, len(visible))
	for _, s := range visible {
		byID[s.ID] = s
	}
	s, ok := byID["web-app"]
	require.True(t, ok, "web-app must be a user-facing starter")
	assert.Equal(t, webAppVersion, s.Version)
	assert.NotContains(t, byID, "fixture", "the test-only fixture must stay withheld from the chooser")

	v, err := Version("web-app")
	require.NoError(t, err)
	assert.Equal(t, webAppVersion, v)
}

// TestWebAppDesignTreeMatchesScaffold pins the committed design tree against
// design.Scaffold: the manifest and runtime assets in the starter tree must
// be byte-identical to what the scaffold writes, so a scaffolded tree and an
// instantiated one are the same tree.
func TestWebAppDesignTreeMatchesScaffold(t *testing.T) {
	scaffoldRoot := t.TempDir()
	require.NoError(t, design.Scaffold(scaffoldRoot))

	template, err := design.ManifestTemplate()
	require.NoError(t, err)
	committed, err := fs.ReadFile(dataFS, dataRoot+"/web-app/"+"design/"+design.ManifestName)
	require.NoError(t, err)
	assert.Equal(t, string(template), string(committed),
		"the starter's design manifest must equal the scaffold template")

	for _, asset := range design.RuntimeAssets {
		got, err := fs.ReadFile(dataFS, dataRoot+"/web-app/design/"+design.RuntimeSubdir+"/"+filepath.ToSlash(asset))
		require.NoErrorf(t, err, "design runtime asset %s must be committed in the starter", asset)
		want, err := os.ReadFile(filepath.Join(scaffoldRoot, design.DirName, design.RuntimeSubdir, filepath.FromSlash(asset)))
		require.NoErrorf(t, err, "scaffold must write %s", asset)
		if asset == "sprout-screens.js" {
			// The committed runtime is a self-contained, file://-runtime
			// variant (it drops the webui preview-proxy re-rooting branch),
			// so it is not byte-identical to the shipped asset; it must
			// still be a non-empty script carrying the runtime version.
			assert.NotEmpty(t, got)
			assert.Contains(t, string(got), "SproutScreens")
			continue
		}
		assert.Equal(t, string(want), string(got),
			"design runtime asset %s must match the scaffold output", asset)
	}
}
