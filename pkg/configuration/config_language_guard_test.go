package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConfigLanguageGuardOnByDefault proves the default: a
// fresh config has the outbound language guard enabled and the opt-out flag
// unset.
func TestNewConfigLanguageGuardOnByDefault(t *testing.T) {
	cfg := NewConfig()

	assert.False(t, cfg.DisableLanguageGuard, "a fresh config must not disable the guard")
	assert.True(t, cfg.LanguageGuardEnabled(), "the guard must be on by default")
}

// TestZeroAndNilConfigLanguageGuardOnByDefault proves the default holds for
// a bare struct (no provenance) and for a nil config.
func TestZeroAndNilConfigLanguageGuardOnByDefault(t *testing.T) {
	assert.True(t, (&Config{}).LanguageGuardEnabled(), "a zero config must default to enabled")
	assert.True(t, (func() *Config { return nil })().LanguageGuardEnabled(),
		"a nil config must resolve to the default: enabled")
}

// TestLoadWithoutConfigFileLanguageGuardOnByDefault proves that with no
// config file at all (the CLI's clean-start case) the guard stays on.
func TestLoadWithoutConfigFileLanguageGuardOnByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.False(t, cfg.DisableLanguageGuard)
	assert.True(t, cfg.LanguageGuardEnabled(), "no config file means the guard is on by default")
}

// TestConfigLanguageGuardJSONRoundTrip proves the JSON contract of the field:
// it serializes under the `disable_language_guard` key when true, is omitted
// when false (omitempty), and unmarshals back.
func TestConfigLanguageGuardJSONRoundTrip(t *testing.T) {
	// Set: the key is present with the value.
	cfg := NewConfig()
	cfg.DisableLanguageGuard = true
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	assert.Equal(t, true, withKey["disable_language_guard"])

	// Unset: the key is omitted (omitempty).
	cfg.DisableLanguageGuard = false
	data, err = json.Marshal(cfg)
	require.NoError(t, err)
	var withoutKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withoutKey))
	assert.NotContains(t, withoutKey, "disable_language_guard",
		"an unset opt-out must be omitted from the serialized config")

	// Unmarshal: an explicit disable is read back.
	var out Config
	require.NoError(t, json.Unmarshal([]byte(`{"disable_language_guard":true}`), &out))
	assert.True(t, out.DisableLanguageGuard)
	assert.False(t, out.LanguageGuardEnabled())
}

// TestConfigLanguageGuardSaveLoadRoundTrip proves the disable value survives
// the config save/load machinery, and that an unset guard setting stays
// enabled across the same round-trip.
func TestConfigLanguageGuardSaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	disabled := NewConfig()
	disabled.DisableLanguageGuard = true
	require.NoError(t, disabled.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.True(t, loaded.DisableLanguageGuard, "an explicit disable must survive save/load")
	assert.False(t, loaded.LanguageGuardEnabled())

	// Round-trip the default: save a config that never named the key and
	// confirm the loaded config still reports the guard enabled.
	enabled := NewConfig()
	require.NoError(t, enabled.Save())

	reloaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.False(t, reloaded.DisableLanguageGuard)
	assert.True(t, reloaded.LanguageGuardEnabled(),
		"an unset setting must round-trip as enabled")
}

// TestLoadRespectsExplicitLanguageGuardOff proves a config file that names
// the key is honored in both directions by Load().
func TestLoadRespectsExplicitLanguageGuardOff(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	configPath := filepath.Join(tmpDir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","disable_language_guard":true}`), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.DisableLanguageGuard)
	assert.False(t, cfg.LanguageGuardEnabled())
}

// TestMergeConfig_DisableLanguageGuard proves the merge convention: an
// override that explicitly disables the guard wins over a base that has it
// enabled, a base disable survives a silent override, and an explicit false
// re-enables the guard over a broader layer's disable.
func TestMergeConfig_DisableLanguageGuard(t *testing.T) {
	// Explicit disable beats an enabled base. The value is true, so it
	// wins even from an in-memory override without provenance.
	result := MergeConfig(&Config{}, &Config{DisableLanguageGuard: true})
	assert.True(t, result.DisableLanguageGuard, "an explicit disable wins over an enabled base")
	assert.False(t, result.LanguageGuardEnabled())

	// A base that disabled the guard keeps it disabled when the override
	// never names the key.
	result = MergeConfig(&Config{DisableLanguageGuard: true}, &Config{})
	assert.True(t, result.DisableLanguageGuard, "a base disable must survive a silent override")
	assert.False(t, result.LanguageGuardEnabled())

	// An explicit false in the override (presence tracked by explicit-key
	// provenance) re-enables the guard over the base disable.
	var override Config
	require.NoError(t, unmarshalLayer([]byte(`{"disable_language_guard":false}`), &override))
	result = MergeConfig(&Config{DisableLanguageGuard: true}, &override)
	assert.False(t, result.DisableLanguageGuard, "an explicit false must re-enable the guard")
	assert.True(t, result.LanguageGuardEnabled())

	// Same explicit-key semantics as the other opt-out booleans: an
	// explicit true is tracked by key presence as well.
	var overrideOn Config
	require.NoError(t, unmarshalLayer([]byte(`{"disable_language_guard":true}`), &overrideOn))
	result = MergeConfig(&Config{}, &overrideOn)
	assert.True(t, result.DisableLanguageGuard)
}

// TestLoadConfigWithLayers_DisableLanguageGuard covers the key across
// layer files, the way the other layer-disable flags are covered: an
// explicit disable in a narrower layer wins over a broader layer's
// enablement, a broader disable persists through a silent narrower layer,
// and the default is on when no layer names the key.
func TestLoadConfigWithLayers_DisableLanguageGuard(t *testing.T) {
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
	assert.False(t, cfg.DisableLanguageGuard)
	assert.True(t, cfg.LanguageGuardEnabled(), "no layer naming the key means the guard is on")

	// Global explicitly enables the guard; the workspace explicitly
	// disables it — the narrower layer's disable must win.
	writeFile(t, globalPath, `{"version":"2.1","disable_language_guard":false}`)
	writeFile(t, workspacePath, `{"disable_language_guard":true}`)
	cfg = load()
	assert.True(t, cfg.DisableLanguageGuard, "workspace disable_language_guard:true must win")
	assert.False(t, cfg.LanguageGuardEnabled())

	// The global disables the guard; the workspace never names the key —
	// the broader disable must persist.
	writeFile(t, globalPath, `{"version":"2.1","disable_language_guard":true}`)
	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.True(t, cfg.DisableLanguageGuard, "a silent workspace layer must keep the global disable")
	assert.False(t, cfg.LanguageGuardEnabled())
	assert.Equal(t, "high", cfg.ReasoningEffort)
}

// TestLanguageGuardEnabledAccessor pins the accessor's default and off
// behavior for later callers that ask "is the guard enabled?".
func TestLanguageGuardEnabledAccessor(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *Config
		wantOn bool
	}{
		{"zero config defaults to on", &Config{}, true},
		{"new config defaults to on", NewConfig(), true},
		{"disabled config reports off", &Config{DisableLanguageGuard: true}, false},
		{"nil config defaults to on", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantOn, tc.cfg.LanguageGuardEnabled())
		})
	}
}
