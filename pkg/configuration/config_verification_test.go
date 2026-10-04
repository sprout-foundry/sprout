package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConfigVerificationOffByDefault proves the SP-149 149e default: a
// fresh config has no verification section (off) and the repair-attempt
// limit resolves to the small default.
func TestNewConfigVerificationOffByDefault(t *testing.T) {
	cfg := NewConfig()

	assert.Nil(t, cfg.Verification, "a fresh config must not enable verification")
	assert.False(t, cfg.VerificationEnabled(), "verification must be off by default")
	assert.Equal(t, DefaultVerificationRepairAttempts, cfg.VerificationRepairAttempts())
}

// TestZeroAndNilConfigVerificationOffByDefault proves the default holds for
// a bare struct (no provenance) and for a nil config.
func TestZeroAndNilConfigVerificationOffByDefault(t *testing.T) {
	assert.False(t, (&Config{}).VerificationEnabled(), "a zero config must default to off")
	assert.Equal(t, DefaultVerificationRepairAttempts, (&Config{}).VerificationRepairAttempts())
	assert.False(t, (func() *Config { return nil })().VerificationEnabled(),
		"a nil config must resolve to off")
	assert.Equal(t, DefaultVerificationRepairAttempts,
		(func() *Config { return nil })().VerificationRepairAttempts())
}

// TestLoadWithoutConfigFileVerificationOffByDefault proves that with no
// config file at all (the CLI's clean-start case) verification stays off.
func TestLoadWithoutConfigFileVerificationOffByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.False(t, cfg.VerificationEnabled(), "no config file means verification is off by default")
	assert.Equal(t, DefaultVerificationRepairAttempts, cfg.VerificationRepairAttempts())
}

// TestConfigVerificationJSONRoundTrip proves the JSON contract of the
// section: the `verification` key is present only when the section was set,
// omits zero-valued fields (omitempty), and unmarshals explicit values in
// both directions.
func TestConfigVerificationJSONRoundTrip(t *testing.T) {
	// Enabled: the key is present with the value.
	cfg := NewConfig()
	cfg.Verification = &VerificationConfig{Enabled: true}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	section, ok := withKey["verification"].(map[string]interface{})
	require.True(t, ok, "an enabled section must be serialized under the verification key")
	assert.Equal(t, true, section["enabled"])
	assert.NotContains(t, section, "repair_attempts", "the zero limit must be omitted")

	// Repair limit: serialized when non-zero.
	cfg = NewConfig()
	cfg.Verification = &VerificationConfig{RepairAttempts: 5}
	data, err = json.Marshal(cfg)
	require.NoError(t, err)
	var withN map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withN))
	section, ok = withN["verification"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), section["repair_attempts"])

	// Unset: a nil section is omitted from the serialized config.
	zero := NewConfig()
	data, err = json.Marshal(zero)
	require.NoError(t, err)
	var withoutKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withoutKey))
	assert.NotContains(t, withoutKey, "verification",
		"an unset section must be omitted from the serialized config")

	// Unmarshal: an explicit enable is read back, as is an explicit
	// disable with the limit set.
	var out Config
	require.NoError(t, json.Unmarshal([]byte(`{"verification":{"enabled":true}}`), &out))
	require.NotNil(t, out.Verification)
	assert.True(t, out.VerificationEnabled())

	var off Config
	require.NoError(t, json.Unmarshal(
		[]byte(`{"verification":{"enabled":false,"repair_attempts":4}}`), &off))
	require.NotNil(t, off.Verification)
	assert.False(t, off.Verification.Enabled)
	assert.Equal(t, 4, off.VerificationRepairAttempts())
}

// TestConfigVerificationSaveLoadRoundTrip proves the enable flag and the
// repair limit survive the config save/load machinery, and that an unset
// section round-trips as off.
func TestConfigVerificationSaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	enabled := NewConfig()
	enabled.Verification = &VerificationConfig{Enabled: true, RepairAttempts: 4}
	require.NoError(t, enabled.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.Verification)
	assert.True(t, loaded.Verification.Enabled, "an explicit enable must survive save/load")
	assert.Equal(t, 4, loaded.VerificationRepairAttempts(),
		"an explicit limit must survive save/load")

	// Round-trip the default: save a config that never named the section
	// and confirm the loaded config still reports verification off.
	defaults := NewConfig()
	require.NoError(t, defaults.Save())

	reloaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.False(t, reloaded.VerificationEnabled(),
		"an unset section must round-trip as off")
	assert.Equal(t, DefaultVerificationRepairAttempts, reloaded.VerificationRepairAttempts())
}

// TestLoadRespectsExplicitVerificationConfig proves a config file that names
// the section is honored in both directions by Load().
func TestLoadRespectsExplicitVerificationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	configPath := filepath.Join(tmpDir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","verification":{"enabled":true}}`), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.VerificationEnabled())

	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"version":"2.1","verification":{"enabled":false}}`), 0600))
	cfg, err = Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.False(t, cfg.VerificationEnabled(), "an explicit disable must be honored")
}

// TestMergeConfig_Verification proves the merge conventions: a truthy
// enable wins over an off base even without provenance, a base enable
// survives a silent override, an explicit false re-disables over a broader
// enable, and the repair limit follows non-zero-wins.
func TestMergeConfig_Verification(t *testing.T) {
	// In-memory override with a truthy enable (no provenance) wins.
	result := MergeConfig(&Config{}, &Config{Verification: &VerificationConfig{Enabled: true}})
	assert.True(t, result.VerificationEnabled())

	// A base enable survives a silent override.
	result = MergeConfig(&Config{Verification: &VerificationConfig{Enabled: true}}, &Config{})
	assert.True(t, result.VerificationEnabled(), "a base enable must survive a silent override")

	// An explicit false (presence tracked by explicit-key provenance)
	// disables a broader layer's enable.
	var override Config
	require.NoError(t, unmarshalLayer([]byte(`{"verification":{"enabled":false}}`), &override))
	result = MergeConfig(&Config{Verification: &VerificationConfig{Enabled: true}}, &override)
	assert.False(t, result.VerificationEnabled(), "an explicit false must disable over a broader enable")

	// An explicit true is tracked by key presence as well.
	var overrideOn Config
	require.NoError(t, unmarshalLayer([]byte(`{"verification":{"enabled":true}}`), &overrideOn))
	result = MergeConfig(&Config{}, &overrideOn)
	assert.True(t, result.VerificationEnabled())

	// A section that names only the limit does not enable the feature.
	var overrideN Config
	require.NoError(t, unmarshalLayer([]byte(`{"verification":{"repair_attempts":5}}`), &overrideN))
	result = MergeConfig(&Config{}, &overrideN)
	assert.False(t, result.VerificationEnabled(), "setting the limit alone must not enable verification")
	assert.Equal(t, 5, result.VerificationRepairAttempts())

	// The limit follows non-zero-wins: an explicit override beats a base
	// limit, and a silent override keeps the base limit.
	base := &Config{Verification: &VerificationConfig{Enabled: true, RepairAttempts: 3}}
	result = MergeConfig(base, &Config{Verification: &VerificationConfig{RepairAttempts: 7}})
	assert.Equal(t, 7, result.VerificationRepairAttempts(), "an explicit limit must beat the base")
	result = MergeConfig(base, &Config{})
	assert.Equal(t, 3, result.VerificationRepairAttempts(), "a silent override must keep the base limit")

	// An empty section is a no-op: it neither allocates nor clears the
	// base section.
	var overrideEmpty Config
	require.NoError(t, unmarshalLayer([]byte(`{"verification":{}}`), &overrideEmpty))
	result = MergeConfig(&Config{}, &overrideEmpty)
	assert.Nil(t, result.Verification, "an empty section must be a no-op on a nil base")
	result = MergeConfig(base, &overrideEmpty)
	assert.True(t, result.VerificationEnabled())
	assert.Equal(t, 3, result.VerificationRepairAttempts())
}

// TestLoadConfigWithLayers_Verification covers the key across layer files,
// the way the other layer flags are covered: a broader enable persists
// through a silent narrower layer, an explicit disable in a narrower layer
// wins, and a per-project (workspace) enable works from a silent global —
// the path an embedding environment uses to enable by default.
func TestLoadConfigWithLayers_Verification(t *testing.T) {
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

	// No layer names the key: verification is off by default.
	cfg := load()
	assert.False(t, cfg.VerificationEnabled(), "no layer naming the key means verification is off")
	assert.Equal(t, DefaultVerificationRepairAttempts, cfg.VerificationRepairAttempts())

	// Global enables verification; the workspace never names the key —
	// the broader enable must persist.
	writeFile(t, globalPath, `{"version":"2.1","verification":{"enabled":true}}`)
	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.True(t, cfg.VerificationEnabled(), "a global enable must persist through a silent workspace")
	assert.Equal(t, DefaultVerificationRepairAttempts, cfg.VerificationRepairAttempts())

	// Per-project enable: the global never names the key, the workspace
	// (project) enables it — the embedding-environment path (149e).
	require.NoError(t, os.Remove(globalPath))
	writeFile(t, workspacePath, `{"verification":{"enabled":true,"repair_attempts":2}}`)
	cfg = load()
	assert.True(t, cfg.VerificationEnabled(), "a workspace enable must turn verification on for the project")
	assert.Equal(t, 2, cfg.VerificationRepairAttempts(), "the project limit must win")

	// The narrower layer disables a broader enable with an explicit false.
	writeFile(t, globalPath, `{"version":"2.1","verification":{"enabled":true}}`)
	writeFile(t, workspacePath, `{"verification":{"enabled":false}}`)
	cfg = load()
	assert.False(t, cfg.VerificationEnabled(), "an explicit workspace disable must beat the global enable")

	// The repair limit stacks across layers: a narrower explicit limit
	// wins, and a silent narrower layer keeps the broader limit.
	writeFile(t, globalPath, `{"version":"2.1","verification":{"enabled":true,"repair_attempts":5}}`)
	writeFile(t, workspacePath, `{"verification":{"repair_attempts":2}}`)
	cfg = load()
	assert.True(t, cfg.VerificationEnabled())
	assert.Equal(t, 2, cfg.VerificationRepairAttempts(), "the narrower limit must win")

	writeFile(t, workspacePath, `{"reasoning_effort":"high"}`)
	cfg = load()
	assert.Equal(t, 5, cfg.VerificationRepairAttempts(), "a silent workspace must keep the global limit")
}

// TestVerificationAccessors pins the accessors' default and configured
// behavior for later items (149.5+) that ask "is verification enabled?" and
// "how many repair attempts are allowed?".
func TestVerificationAccessors(t *testing.T) {
	enabledCases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"zero config defaults to off", &Config{}, false},
		{"new config defaults to off", NewConfig(), false},
		{"nil section defaults to off", &Config{}, false},
		{"enabled section reports on", &Config{Verification: &VerificationConfig{Enabled: true}}, true},
		{"explicit false reports off", &Config{Verification: &VerificationConfig{Enabled: false}}, false},
		{"nil config defaults to off", nil, false},
	}
	for _, tc := range enabledCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.VerificationEnabled())
		})
	}

	attemptCases := []struct {
		name string
		cfg  *Config
		want int
	}{
		{"zero config uses the default", &Config{}, DefaultVerificationRepairAttempts},
		{"new config uses the default", NewConfig(), DefaultVerificationRepairAttempts},
		{"nil section uses the default", &Config{}, DefaultVerificationRepairAttempts},
		{"explicit limit is honored", &Config{Verification: &VerificationConfig{RepairAttempts: 1}}, 1},
		{"non-positive limit falls back", &Config{Verification: &VerificationConfig{RepairAttempts: 0}}, DefaultVerificationRepairAttempts},
		{"negative limit falls back", &Config{Verification: &VerificationConfig{RepairAttempts: -1}}, DefaultVerificationRepairAttempts},
		{"nil config uses the default", nil, DefaultVerificationRepairAttempts},
	}
	for _, tc := range attemptCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.VerificationRepairAttempts())
		})
	}
}

// TestVerificationConfigResolve pins the section's default-resolution:
// nil and zero resolve to off with the default limit, and configured values
// are preserved.
func TestVerificationConfigResolve(t *testing.T) {
	var nilCfg *VerificationConfig
	resolved := nilCfg.Resolve()
	assert.False(t, resolved.Enabled, "a nil section must resolve to off")
	assert.Equal(t, DefaultVerificationRepairAttempts, resolved.RepairAttempts)

	zero := (&VerificationConfig{}).Resolve()
	assert.False(t, zero.Enabled)
	assert.Equal(t, DefaultVerificationRepairAttempts, zero.RepairAttempts,
		"a zero limit must fall back to the default")

	set := (&VerificationConfig{Enabled: true, RepairAttempts: 5}).Resolve()
	assert.True(t, set.Enabled)
	assert.Equal(t, 5, set.RepairAttempts)
}

// TestConfigVerificationCommandsJSON pins the on-disk contract of the
// explicit build/test commands (SP-149 §149b): they are serialized under
// the verification section as build_command/test_command, omitted when
// empty, and read back.
func TestConfigVerificationCommandsJSON(t *testing.T) {
	var in Config
	require.NoError(t, json.Unmarshal([]byte(
		`{"verification":{"build_command":"make build","test_command":"make test"}}`), &in))
	require.NotNil(t, in.Verification)
	assert.Equal(t, "make build", in.VerificationBuildCommand())
	assert.Equal(t, "make test", in.VerificationTestCommand())

	out := NewConfig()
	out.Verification = &VerificationConfig{BuildCommand: "make build"}
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	section, ok := m["verification"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "make build", section["build_command"])
	assert.NotContains(t, section, "test_command", "an unset command must be omitted")
}

// TestMergeConfig_VerificationCommands pins the merge conventions for the
// explicit build/test commands (SP-149 §149b): a non-empty command wins
// (the narrower layer's command beats the broader one), a silent layer
// keeps the broader command, the two commands merge independently, and
// naming commands never enables the feature.
func TestMergeConfig_VerificationCommands(t *testing.T) {
	// A narrower layer's command beats a broader one.
	result := MergeConfig(
		&Config{Verification: &VerificationConfig{BuildCommand: "make build"}},
		&Config{Verification: &VerificationConfig{BuildCommand: "just build"}})
	assert.Equal(t, "just build", result.VerificationBuildCommand(), "the narrower command must win")

	// A silent narrower layer keeps the base command.
	result = MergeConfig(
		&Config{Verification: &VerificationConfig{TestCommand: "make test"}},
		&Config{})
	assert.Equal(t, "make test", result.VerificationTestCommand(),
		"a silent override must keep the base command")

	// The two commands merge independently.
	result = MergeConfig(
		&Config{Verification: &VerificationConfig{BuildCommand: "make build"}},
		&Config{Verification: &VerificationConfig{TestCommand: "make test"}})
	assert.Equal(t, "make build", result.VerificationBuildCommand())
	assert.Equal(t, "make test", result.VerificationTestCommand())

	// A section that names only commands never enables the feature.
	result = MergeConfig(&Config{}, &Config{Verification: &VerificationConfig{
		BuildCommand: "make build", TestCommand: "make test"}})
	assert.False(t, result.VerificationEnabled(), "naming commands alone must not enable verification")
	assert.Equal(t, "make build", result.VerificationBuildCommand())
	assert.Equal(t, "make test", result.VerificationTestCommand())
}

// TestLoadConfigWithLayers_VerificationCommands proves the explicit
// commands flow through the layer files: the project (workspace) layer's
// command wins over the global one, a silent project field keeps the
// global command, and no layer naming the fields resolves to "".
func TestLoadConfigWithLayers_VerificationCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	workspaceDir := filepath.Join(home, "proj", ".sprout")
	globalPath := filepath.Join(globalDir, ConfigFileName)
	workspacePath := filepath.Join(workspaceDir, WorkspaceConfigFileName)

	writeFile(t, globalPath, `{"version":"2.1","verification":{"build_command":"go build ./...","test_command":"go test ./..."}}`)
	writeFile(t, workspacePath, `{"verification":{"build_command":"make build"}}`)

	cfg, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	require.NoError(t, err)
	assert.Equal(t, "make build", cfg.VerificationBuildCommand(), "the project command must win")
	assert.Equal(t, "go test ./...", cfg.VerificationTestCommand(),
		"a silent project field must keep the global command")

	// No layer names the commands: the accessors report "".
	require.NoError(t, os.Remove(globalPath))
	require.NoError(t, os.Remove(workspacePath))
	cfg, err = LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.VerificationBuildCommand())
	assert.Equal(t, "", cfg.VerificationTestCommand())
}
