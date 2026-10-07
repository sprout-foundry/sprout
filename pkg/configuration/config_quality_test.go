package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConfigQualityOffByDefault proves the default: a fresh config has no
// quality section, so the quality-after-edits step is off.
func TestNewConfigQualityOffByDefault(t *testing.T) {
	cfg := NewConfig()

	assert.Nil(t, cfg.Quality, "a fresh config must not enable quality")
	assert.False(t, cfg.QualityEnabled(), "quality must be off by default")
}

// TestZeroAndNilConfigQualityOffByDefault proves the default holds for a bare
// struct (no provenance) and for a nil config.
func TestZeroAndNilConfigQualityOffByDefault(t *testing.T) {
	assert.False(t, (&Config{}).QualityEnabled(), "a zero config must default to off")
	assert.False(t, (func() *Config { return nil })().QualityEnabled(),
		"a nil config must resolve to off")
}

// TestConfigQualityJSONRoundTrip proves the JSON contract of the section: the
// `quality` key is present only when the section was set, omits zero-valued
// fields (omitempty), and unmarshals explicit values in both directions.
func TestConfigQualityJSONRoundTrip(t *testing.T) {
	// Enabled: the key is present with the value.
	cfg := NewConfig()
	cfg.Quality = &QualityConfig{Enabled: true}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	section, ok := withKey["quality"].(map[string]interface{})
	require.True(t, ok, "an enabled section must be serialized under the quality key")
	assert.Equal(t, true, section["enabled"])
	assert.NotContains(t, section, "format_command", "an unset command must be omitted")

	// Commands: serialized when non-empty.
	cfg = NewConfig()
	cfg.Quality = &QualityConfig{FormatCommand: "gofmt -w .", LintCommand: "golangci-lint run"}
	data, err = json.Marshal(cfg)
	require.NoError(t, err)
	var withCmds map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withCmds))
	section, ok = withCmds["quality"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "gofmt -w .", section["format_command"])
	assert.Equal(t, "golangci-lint run", section["lint_command"])

	// Unset: a nil section is omitted.
	zero := NewConfig()
	data, err = json.Marshal(zero)
	require.NoError(t, err)
	var withoutKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withoutKey))
	assert.NotContains(t, withoutKey, "quality", "an unset section must be omitted")

	// Unmarshal: an explicit enable is read back, as is an explicit disable.
	var out Config
	require.NoError(t, json.Unmarshal([]byte(`{"quality":{"enabled":true}}`), &out))
	require.NotNil(t, out.Quality)
	assert.True(t, out.QualityEnabled())

	var off Config
	require.NoError(t, json.Unmarshal(
		[]byte(`{"quality":{"enabled":false,"lint_command":"golangci-lint run"}}`), &off))
	require.NotNil(t, off.Quality)
	assert.False(t, off.Quality.Enabled)
	assert.Equal(t, "golangci-lint run", off.QualityLintCommand())
}

// TestConfigQualitySaveLoadRoundTrip proves the enable flag and the commands
// survive the config save/load machinery, and that an unset section
// round-trips as off.
func TestConfigQualitySaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	enabled := NewConfig()
	enabled.Quality = &QualityConfig{Enabled: true, FormatCommand: "gofmt -w .", LintCommand: "go vet ./..."}
	require.NoError(t, enabled.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.Quality)
	assert.True(t, loaded.Quality.Enabled, "an explicit enable must survive save/load")
	assert.Equal(t, "gofmt -w .", loaded.QualityFormatCommand())
	assert.Equal(t, "go vet ./...", loaded.QualityLintCommand())

	// Round-trip the default: save a config that never named the section and
	// confirm the loaded config still reports quality off.
	defaults := NewConfig()
	require.NoError(t, defaults.Save())

	reloaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.False(t, reloaded.QualityEnabled(), "an unset section must round-trip as off")
}

// TestLoadRespectsExplicitQualityConfig proves a config file that names the
// section is honored in both directions by Load().
func TestLoadRespectsExplicitQualityConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	configPath := filepath.Join(tmpDir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","quality":{"enabled":true}}`), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.QualityEnabled())

	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","quality":{"enabled":false}}`), 0600))
	cfg, err = Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.QualityEnabled(), "an explicit disable must be honored")
}

// TestMergeConfig_Quality proves the merge conventions: a truthy enable wins
// over an off base even without provenance, a base enable survives a silent
// override, an explicit false re-disables over a broader enable, the commands
// follow non-empty-wins, and naming commands alone never enables the feature.
func TestMergeConfig_Quality(t *testing.T) {
	// In-memory override with a truthy enable (no provenance) wins.
	result := MergeConfig(&Config{}, &Config{Quality: &QualityConfig{Enabled: true}})
	assert.True(t, result.QualityEnabled())

	// A base enable survives a silent override.
	result = MergeConfig(&Config{Quality: &QualityConfig{Enabled: true}}, &Config{})
	assert.True(t, result.QualityEnabled(), "a base enable must survive a silent override")

	// An explicit false (presence tracked by explicit-key provenance)
	// disables a broader layer's enable.
	var override Config
	require.NoError(t, unmarshalLayer([]byte(`{"quality":{"enabled":false}}`), &override))
	result = MergeConfig(&Config{Quality: &QualityConfig{Enabled: true}}, &override)
	assert.False(t, result.QualityEnabled(), "an explicit false must disable over a broader enable")

	// An explicit true is tracked by key presence as well.
	var overrideOn Config
	require.NoError(t, unmarshalLayer([]byte(`{"quality":{"enabled":true}}`), &overrideOn))
	result = MergeConfig(&Config{}, &overrideOn)
	assert.True(t, result.QualityEnabled())

	// The commands follow non-empty-wins: an explicit override beats a base
	// command, and a silent override keeps the base command.
	base := &Config{Quality: &QualityConfig{Enabled: true, FormatCommand: "gofmt", LintCommand: "go vet"}}
	result = MergeConfig(base, &Config{Quality: &QualityConfig{FormatCommand: "gofumpt", LintCommand: "staticcheck"}})
	assert.Equal(t, "gofumpt", result.QualityFormatCommand(), "an explicit command must beat the base")
	assert.Equal(t, "staticcheck", result.QualityLintCommand())
	result = MergeConfig(base, &Config{})
	assert.Equal(t, "gofmt", result.QualityFormatCommand(), "a silent override must keep the base command")
	assert.Equal(t, "go vet", result.QualityLintCommand())

	// A section that names only commands never enables the feature.
	var overrideCmds Config
	require.NoError(t, unmarshalLayer([]byte(`{"quality":{"format_command":"gofmt","lint_command":"go vet"}}`), &overrideCmds))
	result = MergeConfig(&Config{}, &overrideCmds)
	assert.False(t, result.QualityEnabled(), "naming commands alone must not enable quality")
	assert.Equal(t, "gofmt", result.QualityFormatCommand())
	assert.Equal(t, "go vet", result.QualityLintCommand())

	// An empty section is a no-op: it neither allocates nor clears the base
	// section.
	var overrideEmpty Config
	require.NoError(t, unmarshalLayer([]byte(`{"quality":{}}`), &overrideEmpty))
	result = MergeConfig(&Config{}, &overrideEmpty)
	assert.Nil(t, result.Quality, "an empty section must be a no-op on a nil base")
	result = MergeConfig(base, &overrideEmpty)
	assert.True(t, result.QualityEnabled())
	assert.Equal(t, "gofmt", result.QualityFormatCommand())
}

// TestLoadConfigWithLayers_Quality covers the key across layer files: a global
// enable persists through a silent workspace, an explicit workspace disable
// wins, and a per-project (workspace) enable works from a silent global.
func TestLoadConfigWithLayers_Quality(t *testing.T) {
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

	// No layer names the key: quality is off by default.
	cfg := load()
	assert.False(t, cfg.QualityEnabled(), "no layer naming the key means quality is off")

	// Global enables quality; the workspace never names the key — the
	// broader enable must persist.
	writeFile(t, globalPath, `{"version":"2.1","quality":{"enabled":true}}`)
	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.True(t, cfg.QualityEnabled(), "a global enable must persist through a silent workspace")

	// Per-project enable: the global never names the key, the workspace
	// (project) enables it.
	require.NoError(t, os.Remove(globalPath))
	writeFile(t, workspacePath, `{"quality":{"enabled":true,"format_command":"gofmt -w ."}}`)
	cfg = load()
	assert.True(t, cfg.QualityEnabled(), "a workspace enable must turn quality on for the project")
	assert.Equal(t, "gofmt -w .", cfg.QualityFormatCommand())

	// The narrower layer disables a broader enable with an explicit false.
	writeFile(t, globalPath, `{"version":"2.1","quality":{"enabled":true}}`)
	writeFile(t, workspacePath, `{"quality":{"enabled":false}}`)
	cfg = load()
	assert.False(t, cfg.QualityEnabled(), "an explicit workspace disable must beat the global enable")

	// The commands stack across layers: a narrower explicit command wins, a
	// silent narrower layer keeps the broader command.
	writeFile(t, globalPath, `{"version":"2.1","quality":{"enabled":true,"lint_command":"go vet"}}`)
	writeFile(t, workspacePath, `{"quality":{"format_command":"gofmt"}}`)
	cfg = load()
	assert.True(t, cfg.QualityEnabled())
	assert.Equal(t, "gofmt", cfg.QualityFormatCommand(), "the narrower command must win")
	assert.Equal(t, "go vet", cfg.QualityLintCommand(), "the broader unsilenced command must persist")

	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.Equal(t, "go vet", cfg.QualityLintCommand(), "a silent workspace must keep the global command")
}

// TestQualityAccessors pins the accessors' default and configured behavior:
// a nil section reports off with no commands, and configured values are read
// back.
func TestQualityAccessors(t *testing.T) {
	enabledCases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"zero config defaults to off", &Config{}, false},
		{"new config defaults to off", NewConfig(), false},
		{"enabled section reports on", &Config{Quality: &QualityConfig{Enabled: true}}, true},
		{"explicit false reports off", &Config{Quality: &QualityConfig{Enabled: false}}, false},
		{"nil config defaults to off", nil, false},
	}
	for _, tc := range enabledCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.QualityEnabled())
		})
	}

	cfg := &Config{Quality: &QualityConfig{FormatCommand: "gofmt", LintCommand: "go vet"}}
	assert.Equal(t, "gofmt", cfg.QualityFormatCommand())
	assert.Equal(t, "go vet", cfg.QualityLintCommand())
	assert.Equal(t, "", (&Config{}).QualityFormatCommand())
	assert.Equal(t, "", (&Config{}).QualityLintCommand())
	assert.Equal(t, "", (func() *Config { return nil })().QualityFormatCommand())
}
