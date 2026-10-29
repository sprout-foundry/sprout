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

// TestRepetitionGuardReasoningDefaults pins the reasoning-channel defaults:
// on by default with the tighter thresholds, and a nil config resolves to the
// same values.
func TestRepetitionGuardReasoningDefaults(t *testing.T) {
	cfg := NewConfig()
	assert.True(t, cfg.RepetitionGuardReasoningEnabled(), "the reasoning guard must be on by default")
	assert.Equal(t, DefaultRepetitionReasoningMinRepetitions, cfg.RepetitionGuardReasoningMinRepetitions())
	assert.Equal(t, DefaultRepetitionReasoningMaxLineChars, cfg.RepetitionGuardReasoningMaxLineChars())
	assert.Less(t, DefaultRepetitionReasoningMinRepetitions, DefaultRepetitionMinRepetitions,
		"the reasoning channel must flag sooner than visible prose")

	var nilCfg *Config
	assert.True(t, nilCfg.RepetitionGuardReasoningEnabled())
	assert.Equal(t, DefaultRepetitionReasoningMinRepetitions, nilCfg.RepetitionGuardReasoningMinRepetitions())
	assert.Equal(t, DefaultRepetitionReasoningMaxLineChars, nilCfg.RepetitionGuardReasoningMaxLineChars())
}

// TestRepetitionGuardReasoningMerge proves a narrower config layer can set the
// reasoning thresholds and disable the reasoning guard without disturbing the
// visible-text settings.
func TestRepetitionGuardReasoningMerge(t *testing.T) {
	base := NewConfig()
	base.RepetitionGuard = &RepetitionGuardConfig{MinRepetitions: 6, MaxLineChars: 120}

	override := NewConfig()
	override.RepetitionGuard = &RepetitionGuardConfig{
		ReasoningEnabled:        boolPtr(false),
		ReasoningMinRepetitions: 3,
		ReasoningMaxLineChars:   50,
	}

	merged := MergeConfig(base, override)

	assert.Equal(t, 6, merged.RepetitionGuardMinRepetitions(), "visible-text threshold must survive")
	assert.Equal(t, 120, merged.RepetitionGuardMaxLineChars())
	assert.False(t, merged.RepetitionGuardReasoningEnabled())
	assert.Equal(t, 3, merged.RepetitionGuardReasoningMinRepetitions())
	assert.Equal(t, 50, merged.RepetitionGuardReasoningMaxLineChars())
}

// TestRepetitionGuardRecoveryDefaults pins the recovery-compaction
// escalation's defaults: on by default (a nil section or a nil config both
// resolve to enabled), the LLM-summary half on by default, and the summarizer
// effort defaulting to "low".
func TestRepetitionGuardRecoveryDefaults(t *testing.T) {
	cfg := NewConfig()
	assert.True(t, cfg.RepetitionGuardRecoveryEnabled(), "the recovery escalation must be on by default")
	assert.True(t, cfg.RepetitionGuardRecoverySummary(), "the recovery LLM summary must be on by default")
	assert.Equal(t, "low", cfg.RepetitionGuardRecoverySummarizerEffort())

	var nilCfg *Config
	assert.True(t, nilCfg.RepetitionGuardRecoveryEnabled())
	assert.True(t, nilCfg.RepetitionGuardRecoverySummary())
	assert.Equal(t, "low", nilCfg.RepetitionGuardRecoverySummarizerEffort())
}

// TestRepetitionGuardRecoveryOverrides pins that a narrower layer can disable
// the escalation or the summary, and can raise the summarizer effort; an
// unrecognized effort falls back to "low".
func TestRepetitionGuardRecoveryOverrides(t *testing.T) {
	// Disable the whole escalation.
	cfg := NewConfig()
	cfg.RepetitionGuard = &RepetitionGuardConfig{Recovery: &RepetitionRecoveryConfig{Enabled: boolPtr(false)}}
	assert.False(t, cfg.RepetitionGuardRecoveryEnabled())
	assert.True(t, cfg.RepetitionGuardRecoverySummary(), "a silent summary key keeps its default (on)")

	// Disable only the LLM-summary half (the de-loop still runs).
	cfg = NewConfig()
	cfg.RepetitionGuard = &RepetitionGuardConfig{Recovery: &RepetitionRecoveryConfig{Summary: boolPtr(false)}}
	assert.True(t, cfg.RepetitionGuardRecoveryEnabled())
	assert.False(t, cfg.RepetitionGuardRecoverySummary())

	// A recognized effort passes through; an unrecognized one falls back.
	cfg = NewConfig()
	cfg.RepetitionGuard = &RepetitionGuardConfig{Recovery: &RepetitionRecoveryConfig{SummarizerEffort: "high"}}
	assert.Equal(t, "high", cfg.RepetitionGuardRecoverySummarizerEffort())
	cfg.RepetitionGuard.Recovery.SummarizerEffort = "ultra"
	assert.Equal(t, "low", cfg.RepetitionGuardRecoverySummarizerEffort(), "an unrecognized effort must fall back to the default")
}

// TestRepetitionGuardRecoveryJSONRoundTrip proves the recovery block's JSON
// contract: the keys are omitted when unset and read back through the
// accessors when set.
func TestRepetitionGuardRecoveryJSONRoundTrip(t *testing.T) {
	// Set block: all keys present.
	cfg := NewConfig()
	cfg.RepetitionGuard = &RepetitionGuardConfig{
		Recovery: &RepetitionRecoveryConfig{
			Enabled:          boolPtr(false),
			Summary:          boolPtr(false),
			SummarizerEffort: "high",
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	section := withKey["repetition_guard"].(map[string]interface{})
	recovery := section["recovery"].(map[string]interface{})
	assert.Equal(t, false, recovery["enabled"])
	assert.Equal(t, false, recovery["summary"])
	assert.Equal(t, "high", recovery["summarizer_effort"])

	// Unset: a nil recovery block is omitted.
	zero := NewConfig()
	zero.RepetitionGuard = &RepetitionGuardConfig{Enabled: boolPtr(true)}
	data, err = json.Marshal(zero)
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &out))
	section = out["repetition_guard"].(map[string]interface{})
	assert.NotContains(t, section, "recovery", "an unset recovery block must be omitted")

	// Unmarshal: explicit values are read back through the accessors.
	var loaded Config
	require.NoError(t, json.Unmarshal(
		[]byte(`{"repetition_guard":{"recovery":{"enabled":true,"summary":false,"summarizer_effort":"medium"}}}`),
		&loaded))
	assert.True(t, loaded.RepetitionGuardRecoveryEnabled())
	assert.False(t, loaded.RepetitionGuardRecoverySummary())
	assert.Equal(t, "medium", loaded.RepetitionGuardRecoverySummarizerEffort())
}
