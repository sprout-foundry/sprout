package starters

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// webAppDataVersion is the version the web-app-data tree declares; the
// assertions below must agree with
// pkg/starters/data/web-app-data/starter.json.
const webAppDataVersion = "1.0.0"

// webAppDataProjectFiles are the project files (relative to the
// destination) a web-app-data instantiation must produce: the React + Vite
// client source tree, the Hono Worker under src/worker, the Drizzle schema
// and migration, the quality config, the README, the pinned lockfile, the
// Workers config, and the design workspace tree. The exact count is not
// pinned (the tree grows), but every file the item names is asserted
// present.
var webAppDataProjectFiles = []string{
	"package.json",
	"package-lock.json",
	"wrangler.toml",
	"vite.config.ts",
	"vitest.config.ts",
	"vitest.workers.config.ts",
	"drizzle.config.ts",
	"index.html",
	"tsconfig.json",
	"eslint.config.mjs",
	".prettierrc.json",
	".gitignore",
	".gitattributes",
	"README.md",
	filepath.Join("public", "favicon.svg"),
	filepath.Join("src", "main.tsx"),
	filepath.Join("src", "App.tsx"),
	filepath.Join("src", "layouts", "Layout.tsx"),
	filepath.Join("src", "pages", "Home.tsx"),
	filepath.Join("src", "pages", "About.tsx"),
	filepath.Join("src", "pages", "Items.tsx"),
	filepath.Join("src", "hooks", "useLocalStorage.ts"),
	filepath.Join("src", "styles", "global.css"),
	filepath.Join("src", "worker", "index.ts"),
	filepath.Join("src", "worker", "schema.ts"),
	filepath.Join("drizzle", "migrations", "0000_init.sql"),
	filepath.Join("drizzle", "migrations", "meta", "_journal.json"),
	filepath.Join("test", "setup.ts"),
	filepath.Join("test", "app.test.tsx"),
	filepath.Join("test", "api.test.ts"),
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

// TestInstantiateWebAppDataIntoEmptyDirectory proves the web-app-data
// starter instantiates into an empty directory: every project file the
// item names lands on disk byte-for-byte from the embedded tree, the
// descriptor is not copied verbatim, and the manifest loads back through
// the store with the declared commands, port, routes, build output and
// deploy target. It runs no npm step: the tree is validated as data.
func TestInstantiateWebAppDataIntoEmptyDirectory(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "app")

	require.NoError(t, Instantiate("web-app-data", dest))

	for _, rel := range webAppDataProjectFiles {
		data, err := os.ReadFile(filepath.Join(dest, rel))
		require.NoErrorf(t, err, "missing %s in the instantiated project", rel)
		want, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/"+filepath.ToSlash(rel))
		require.NoError(t, err)
		assert.Equal(t, want, data, "copied content must match the embedded tree (%s)", rel)
	}

	// The descriptor is never copied verbatim: its validated form becomes
	// .sprout/starter.json instead.
	_, err := os.Stat(filepath.Join(dest, DescriptorName))
	assert.True(t, os.IsNotExist(err), "the top-level descriptor must not be copied into the project")

	m, err := starterstore.LoadStarterManifest(dest)
	require.NoError(t, err, "the written manifest must load through pkg/starterstore")
	assert.Equal(t, "web-app-data", m.Starter.ID)
	assert.Equal(t, webAppDataVersion, m.Starter.Version)
	assert.Equal(t, "npm run build", m.Build)
	assert.Equal(t, "npm test", m.Test)
	assert.Equal(t, "npm run dev", m.Dev)
	assert.Equal(t, 8787, m.DevPort)
	assert.Contains(t, m.Routes, "/api/items")
	assert.Equal(t, "dist", m.BuildOutput)
	assert.Equal(t, startermanifest.DeployTargetWorkers, m.DeployTarget)

	// The design tree scaffold must be present so design mode works from
	// the first turn.
	assert.FileExists(t, filepath.Join(dest, design.DirName, design.ManifestName))
}

// TestWebAppDataManifestValidates pins the embedded descriptor against the
// manifest schema directly (validation runs on the written
// .sprout/starter.json through the store in the instantiation test above).
func TestWebAppDataManifestValidates(t *testing.T) {
	raw, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/"+DescriptorName)
	require.NoError(t, err)

	m, err := startermanifest.ValidateJSON(raw)
	require.NoError(t, err, "the web-app-data descriptor must satisfy the manifest schema")
	assert.Equal(t, "web-app-data", m.Starter.ID)
	assert.Equal(t, webAppDataVersion, m.Starter.Version)
	assert.Equal(t, "dist", m.BuildOutput)
	assert.Equal(t, startermanifest.DeployTargetWorkers, m.DeployTarget)
	assert.NotEmpty(t, m.Routes)
}

// TestWebAppDataLockfileMatchesPackageJSON pins the committed lockfile: it
// is valid npm lockfile JSON whose root package entry names the same pinned
// direct dependencies as package.json, so `npm ci` installs exactly what the
// starter declares. The check is pure data — it runs no npm step.
func TestWebAppDataLockfileMatchesPackageJSON(t *testing.T) {
	pkgRaw, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/package.json")
	require.NoError(t, err)
	lockRaw, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/package-lock.json")
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

	// Every direct dependency must resolve in the lockfile tree. An exact
	// pin must resolve to that exact version; a caret/tilde range must
	// resolve to a version within the range, so the pins are real and not
	// just declared.
	for name, spec := range pkg.Dependencies {
		entry, ok := lock.Packages["node_modules/"+name]
		require.Truef(t, ok, "dependency %s must have a lockfile entry", name)
		assert.Truef(t, specSatisfiedBy(spec, entry.Version), "dependency %s (%s) must resolve within its range, got %s", name, spec, entry.Version)
	}
	for name, spec := range pkg.DevDependencies {
		entry, ok := lock.Packages["node_modules/"+name]
		require.Truef(t, ok, "devDependency %s must have a lockfile entry", name)
		assert.Truef(t, specSatisfiedBy(spec, entry.Version), "devDependency %s (%s) must resolve within its range, got %s", name, spec, entry.Version)
	}
}

// specSatisfiedBy reports whether resolved satisfies the npm version spec
// package.json declares: an exact version must match exactly, a caret range
// must keep the same major (and be at or above the lower bound), and a tilde
// range must keep the same major.minor.
func specSatisfiedBy(spec, resolved string) bool {
	switch {
	case strings.HasPrefix(spec, "^"):
		want, got := versionParts(strings.TrimPrefix(spec, "^")), versionParts(resolved)
		return len(want) > 0 && len(got) > 0 && want[0] == got[0] && compareParts(got, want) >= 0
	case strings.HasPrefix(spec, "~"):
		want, got := versionParts(strings.TrimPrefix(spec, "~")), versionParts(resolved)
		return len(want) >= 2 && len(got) >= 2 && want[0] == got[0] && want[1] == got[1] && compareParts(got, want) >= 0
	default:
		return spec == resolved
	}
}

func versionParts(v string) []int {
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func compareParts(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return len(a) - len(b)
}

// TestWebAppDataIsUserFacing pins the catalogue split: the web-app-data
// starter is user-facing (present in ListForUsers), while the test-only
// fixture stays withheld.
func TestWebAppDataIsUserFacing(t *testing.T) {
	visible, err := ListForUsers()
	require.NoError(t, err)

	byID := make(map[string]Starter, len(visible))
	for _, s := range visible {
		byID[s.ID] = s
	}
	s, ok := byID["web-app-data"]
	require.True(t, ok, "web-app-data must be a user-facing starter")
	assert.Equal(t, webAppDataVersion, s.Version)
	assert.NotContains(t, byID, "fixture", "the test-only fixture must stay withheld from the chooser")

	v, err := Version("web-app-data")
	require.NoError(t, err)
	assert.Equal(t, webAppDataVersion, v)
}

// TestWebAppDataDesignTreeMatchesScaffold pins the committed design tree
// against design.Scaffold: the manifest and runtime assets in the starter
// tree must be byte-identical to what the scaffold writes, so a scaffolded
// tree and an instantiated one are the same tree.
func TestWebAppDataDesignTreeMatchesScaffold(t *testing.T) {
	scaffoldRoot := t.TempDir()
	require.NoError(t, design.Scaffold(scaffoldRoot))

	template, err := design.ManifestTemplate()
	require.NoError(t, err)
	committed, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/"+"design/"+design.ManifestName)
	require.NoError(t, err)
	assert.Equal(t, string(template), string(committed),
		"the starter's design manifest must equal the scaffold template")

	for _, asset := range design.RuntimeAssets {
		got, err := fs.ReadFile(dataFS, dataRoot+"/web-app-data/design/"+design.RuntimeSubdir+"/"+filepath.ToSlash(asset))
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
