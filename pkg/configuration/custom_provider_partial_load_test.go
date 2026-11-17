package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The providers dir is hand-seeded and volume-mounted; a stray or
// half-written file is normal. One bad file must never hide the healthy
// providers (the original behavior returned an error and every caller threw
// away the whole directory — a provider named "My.Gateway.json" or a stray
// backup silently erased every other provider).

func writeProviderFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
}

const validProviderJSON = `{
  "name": "good-provider",
  "endpoint": "https://example.com/v1",
  "model_name": "good-model"
}`

func TestLoadCustomProvidersFromDir_SkipsBadFileKeepsGood(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "providers")
	writeProviderFiles(t, dir, map[string]string{
		"good.json":          validProviderJSON,
		"broken-syntax.json": `{"name": "broken", "endpoint":`,
		"invalid-name.json":  `{"name": "My.Gateway", "endpoint": "https://example.com/v1"}`,
	})

	providers, err := LoadCustomProvidersFromDir(dir)
	// The error is a warning that names the skipped files...
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken-syntax.json")
	assert.Contains(t, err.Error(), "invalid-name.json")
	assert.Contains(t, err.Error(), dir)
	// ...but the healthy provider still loads.
	require.Contains(t, providers, "good-provider")
	assert.Equal(t, "good-model", providers["good-provider"].ModelName)
}

func TestLoadCustomProvidersFromDir_AllGoodNoError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "providers")
	writeProviderFiles(t, dir, map[string]string{
		"good.json":     validProviderJSON,
		"second.json":   `{"name": "second-provider", "endpoint": "https://example.com/v1"}`,
		"notes-ignored": "not json at all", // not *.json: not even globbed
	})

	providers, err := LoadCustomProvidersFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, providers, 2)
}

func TestLoadCustomProvidersFromDir_MissingDirIsNotAnError(t *testing.T) {
	providers, err := LoadCustomProvidersFromDir(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, providers)
}

func TestLoadCustomProviders_OneBadFileKeepsTheRest(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)
	writeProviderFiles(t, providersDir, map[string]string{
		"good.json":      validProviderJSON,
		"stray.json":     `[]`, // valid JSON, wrong shape: no name → invalid
		"uppercase.json": `{"name": "Bad.Name", "endpoint": "https://example.com/v1"}`,
	})

	providers, err := LoadCustomProviders()
	require.NoError(t, err) // the warning is for the skipped files only
	require.Contains(t, providers, "good-provider")
}

// The whole point of saving providers globally is that every scope sees
// them; an enrichment path that reads only the scoped dir broke that.
func TestEnrichCustomProviders_SeesGlobalProviderUnderScopedConfig(t *testing.T) {
	globalHome := t.TempDir()
	scopedDir := t.TempDir()
	t.Setenv("HOME", globalHome)
	t.Setenv("SPROUT_CONFIG", scopedDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	globalProvidersDir, err := GetGlobalProvidersDir()
	require.NoError(t, err)
	writeProviderFiles(t, globalProvidersDir, map[string]string{"good.json": validProviderJSON})

	m, err := NewManagerWithLayers(scopedDir, "")
	require.NoError(t, err)
	m.EnrichCustomProviders()

	cfg := m.GetConfig()
	require.Contains(t, cfg.CustomProviders, "good-provider",
		"a globally saved provider must survive an isolated (scoped) config")
}

func TestEnrichCustomProviders_MergesPartial(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	providersDir, err := GetGlobalProvidersDir()
	require.NoError(t, err)
	writeProviderFiles(t, providersDir, map[string]string{
		"good.json":   validProviderJSON,
		"broken.json": `{"name": "broken"`,
	})

	m, err := NewManagerWithLayers(globalHome, "")
	require.NoError(t, err)
	m.EnrichCustomProviders()

	cfg := m.GetConfig()
	require.Contains(t, cfg.CustomProviders, "good-provider")
}

// Load() must stay alive over a stray file: an error here once made every
// command fail to start.
func TestLoad_SurvivesStrayProviderFile(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("HOME", globalHome)
	scoped := t.TempDir()
	t.Setenv("SPROUT_CONFIG", scoped)
	t.Setenv("XDG_CONFIG_HOME", "")

	require.NoError(t, os.WriteFile(filepath.Join(scoped, ConfigFileName), []byte(`{"version":"2.0"}`), 0o600))
	providersDir := filepath.Join(globalHome, ".config", "sprout", "providers")
	writeProviderFiles(t, providersDir, map[string]string{
		"good.json":  validProviderJSON,
		"stray.json": `{oops`,
	})

	cfg, err := Load()
	require.NoError(t, err)
	require.Contains(t, cfg.CustomProviders, "good-provider")
	assert.NotContains(t, cfg.CustomProviders, "stray")
}

// The listing (layered manager) and the creation path (LoadCustomProviders)
// must resolve the same definition for a same-named provider, or switching
// to what the listing shows can silently use a different endpoint.
func TestSameNamedProviderSameWinnerAcrossPaths(t *testing.T) {
	globalHome := t.TempDir()
	scopedDir := t.TempDir()
	t.Setenv("HOME", globalHome)
	t.Setenv("SPROUT_CONFIG", scopedDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	writeProviderFiles(t, filepath.Join(globalHome, ".config", "sprout", "providers"), map[string]string{
		"mine.json": `{"name": "mine", "endpoint": "https://global.example.com/v1", "model_name": "global-model"}`,
	})
	writeProviderFiles(t, filepath.Join(scopedDir, "providers"), map[string]string{
		"mine.json": `{"name": "mine", "endpoint": "https://project.example.com/v1", "model_name": "project-model"}`,
	})

	fromFiles, err := LoadCustomProviders()
	require.NoError(t, err)

	m, err := NewManagerWithLayers(scopedDir, "")
	require.NoError(t, err)
	cfg := m.GetConfig()

	require.Contains(t, fromFiles, "mine")
	require.Contains(t, cfg.CustomProviders, "mine")
	assert.Equal(t, cfg.CustomProviders["mine"].Endpoint, fromFiles["mine"].Endpoint,
		"the manager and LoadCustomProviders must agree on which same-named copy wins")
	assert.Equal(t, cfg.CustomProviders["mine"].ModelName, fromFiles["mine"].ModelName)
}
