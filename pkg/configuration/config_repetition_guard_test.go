package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepetitionGuardEnabledByDefault proves the default: a fresh config has
// no repetition_guard section, so the guard is on with sane thresholds.
func TestRepetitionGuardEnabledByDefault(t *testing.T) {
	cfg := NewConfig()
	assert.Nil(t, cfg.RepetitionGuard, "a fresh config must not need a repetition_guard section")
	assert.True(t, cfg.RepetitionGuardEnabled(), "the repetition guard must be on by default")
	assert.Equal(t, DefaultRepetitionMinRepetitions, cfg.RepetitionGuardMinRepetitions())
	assert.Equal(t, DefaultRepetitionMaxLineChars, cfg.RepetitionGuardMaxLineChars())

	// A nil config resolves to the same defaults.
	var nilCfg *Config
	assert.True(t, nilCfg.RepetitionGuardEnabled())
	assert.Equal(t, DefaultRepetitionMinRepetitions, nilCfg.RepetitionGuardMinRepetitions())
	assert.Equal(t, DefaultRepetitionMaxLineChars, nilCfg.RepetitionGuardMaxLineChars())
}

// TestConfigRepetitionGuardJSONRoundTrip proves the JSON contract of the
// section: the key is present only when set, the pointers/zero values omit
// correctly, and explicit values unmarshal in both directions.
func TestConfigRepetitionGuardJSONRoundTrip(t *testing.T) {
	cfg := NewConfig()
	cfg.RepetitionGuard = &RepetitionGuardConfig{Enabled: boolPtr(false), MinRepetitions: 8, MaxLineChars: 200}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	section, ok := withKey["repetition_guard"].(map[string]interface{})
	require.True(t, ok, "a set section must be serialized under the repetition_guard key")
	assert.Equal(t, false, section["enabled"])
	assert.Equal(t, float64(8), section["min_repetitions"])
	assert.Equal(t, float64(200), section["max_line_chars"])

	// Unset: a nil section is omitted.
	zero := NewConfig()
	data, err = json.Marshal(zero)
	require.NoError(t, err)
	var withoutKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withoutKey))
	assert.NotContains(t, withoutKey, "repetition_guard", "an unset section must be omitted")

	// Unmarshal: explicit values are read back through the accessors.
	var out Config
	require.NoError(t, json.Unmarshal(
		[]byte(`{"repetition_guard":{"enabled":false,"min_repetitions":3,"max_line_chars":60}}`), &out))
	require.NotNil(t, out.RepetitionGuard)
	assert.False(t, out.RepetitionGuardEnabled())
	assert.Equal(t, 3, out.RepetitionGuardMinRepetitions())
	assert.Equal(t, 60, out.RepetitionGuardMaxLineChars())
}

// TestRepetitionGuardAccessorClamping pins the accessors' fallbacks: a
// below-2 min-repetitions and a non-positive max-line-chars fall back to the
// defaults.
func TestRepetitionGuardAccessorClamping(t *testing.T) {
	cfg := &Config{RepetitionGuard: &RepetitionGuardConfig{Enabled: boolPtr(true), MinRepetitions: 1, MaxLineChars: 0}}
	assert.Equal(t, DefaultRepetitionMinRepetitions, cfg.RepetitionGuardMinRepetitions())
	assert.Equal(t, DefaultRepetitionMaxLineChars, cfg.RepetitionGuardMaxLineChars())
}

// TestMergeConfig_RepetitionGuard proves the merge conventions: a disable wins
// (explicit or a present pointer), a base disable survives a silent override, a
// base enable survives a silent override, and the thresholds follow
// non-zero-wins.
func TestMergeConfig_RepetitionGuard(t *testing.T) {
	// A present *bool false disables even without provenance.
	result := MergeConfig(&Config{}, &Config{RepetitionGuard: &RepetitionGuardConfig{Enabled: boolPtr(false)}})
	assert.False(t, result.RepetitionGuardEnabled())

	// A base disable survives a silent override.
	result = MergeConfig(&Config{RepetitionGuard: &RepetitionGuardConfig{Enabled: boolPtr(false)}}, &Config{})
	assert.False(t, result.RepetitionGuardEnabled())

	// An explicit enable (present pointer) turns it on.
	result = MergeConfig(&Config{}, &Config{RepetitionGuard: &RepetitionGuardConfig{Enabled: boolPtr(true)}})
	assert.True(t, result.RepetitionGuardEnabled())

	// Thresholds follow non-zero-wins: an explicit override beats a base value,
	// a silent override keeps the base value.
	base := &Config{RepetitionGuard: &RepetitionGuardConfig{MinRepetitions: 4, MaxLineChars: 80}}
	result = MergeConfig(base, &Config{RepetitionGuard: &RepetitionGuardConfig{MinRepetitions: 9}})
	assert.Equal(t, 9, result.RepetitionGuardMinRepetitions(), "an explicit threshold must beat the base")
	assert.Equal(t, 80, result.RepetitionGuardMaxLineChars(), "a silent threshold must keep the base")

	// An empty section is a no-op on a nil base.
	var overrideEmpty Config
	require.NoError(t, unmarshalLayer([]byte(`{"repetition_guard":{}}`), &overrideEmpty))
	result = MergeConfig(&Config{}, &overrideEmpty)
	assert.Nil(t, result.RepetitionGuard, "an empty section must be a no-op on a nil base")
}

// TestLoadConfigWithLayers_RepetitionGuard covers the key across layer files: a
// global disable persists through a silent workspace, and a workspace enable
// overrides a global disable.
func TestLoadConfigWithLayers_RepetitionGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	workspaceDir := filepath.Join(home, "proj", ".sprout")
	globalPath := filepath.Join(globalDir, ConfigFileName)
	workspacePath := filepath.Join(workspaceDir, WorkspaceConfigFileName)

	load := func() *Config {
		cfg, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
		require.NoError(t, err)
		return cfg
	}

	// No layer names the key: the guard is on by default.
	cfg := load()
	assert.True(t, cfg.RepetitionGuardEnabled(), "no layer naming the key means the guard is on")

	// Global disables; the workspace never names the key — the disable persists.
	writeFile(t, globalPath, `{"version":"2.1","repetition_guard":{"enabled":false}}`)
	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.False(t, cfg.RepetitionGuardEnabled(), "a global disable must persist through a silent workspace")

	// The workspace (project) re-enables over the global disable.
	writeFile(t, workspacePath, `{"repetition_guard":{"enabled":true,"min_repetitions":5}}`)
	cfg = load()
	assert.True(t, cfg.RepetitionGuardEnabled(), "a workspace enable must override the global disable")
	assert.Equal(t, 5, cfg.RepetitionGuardMinRepetitions())
}

// TestRepetitionGuardSaveLoadRoundTrip proves an explicit section survives the
// config save/load machinery, and an unset section round-trips as the default.
func TestRepetitionGuardSaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	configured := NewConfig()
	configured.RepetitionGuard = &RepetitionGuardConfig{Enabled: boolPtr(false), MinRepetitions: 4}
	require.NoError(t, configured.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.False(t, loaded.RepetitionGuardEnabled(), "an explicit disable must survive save/load")
	assert.Equal(t, 4, loaded.RepetitionGuardMinRepetitions())

	defaults := NewConfig()
	require.NoError(t, defaults.Save())
	reloaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.True(t, reloaded.RepetitionGuardEnabled(), "an unset section must round-trip as enabled")
}

// TestRepetitionGuardConfigPath pins that the section round-trips through
// os.WriteFile/Load with an explicit file (no reliance on the in-memory merge).
func TestRepetitionGuardConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	configPath := filepath.Join(tmpDir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","repetition_guard":{"enabled":false,"min_repetitions":7}}`), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.RepetitionGuardEnabled())
	assert.Equal(t, 7, cfg.RepetitionGuardMinRepetitions())
}
