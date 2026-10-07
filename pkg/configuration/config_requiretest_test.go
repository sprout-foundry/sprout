package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireTestOffByDefault proves the opt-in default: a fresh, zero, and
// nil config all report the require-a-test flag off, and an unset section
// omits the field from JSON.
func TestRequireTestOffByDefault(t *testing.T) {
	assert.False(t, NewConfig().RequireTest(), "a fresh config must not require a test")
	assert.False(t, (&Config{}).RequireTest(), "a zero config must default to off")
	assert.False(t, (func() *Config { return nil })().RequireTest(),
		"a nil config must resolve to off")
	assert.False(t, (&Config{Verification: &VerificationConfig{Enabled: true}}).RequireTest(),
		"enabling verification must not silently require a test")

	data, err := json.Marshal(NewConfig())
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.NotContains(t, m, "verification", "an unset section must be omitted")
}

// TestRequireTestJSONRoundTrip proves the flag's JSON contract: it is
// serialized under verification.require_test only when set, and read back in
// both directions.
func TestRequireTestJSONRoundTrip(t *testing.T) {
	cfg := NewConfig()
	cfg.Verification = &VerificationConfig{Enabled: true, RequireTest: true}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	section, ok := m["verification"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, section["require_test"])

	var out Config
	require.NoError(t, json.Unmarshal([]byte(`{"verification":{"require_test":true}}`), &out))
	require.NotNil(t, out.Verification)
	assert.True(t, out.RequireTest())
}

// TestMergeRequireTest proves the layer-merge semantics of the flag: an
// explicit true in a narrower layer enables it, an explicit false disables a
// broader layer's enable, and a layer that never names it keeps the broader
// value.
func TestMergeRequireTest(t *testing.T) {
	on := func() *Config {
		c := &Config{Verification: &VerificationConfig{RequireTest: true}}
		c.explicitKeys = map[string]bool{"verification.require_test": true}
		return c
	}

	// Narrower layer names the key true: it enables.
	result := MergeConfig(&Config{}, on())
	assert.True(t, result.RequireTest(), "an explicit true must enable the flag")

	// Narrower layer names the key false: it disables the broader enable.
	off := &Config{Verification: &VerificationConfig{RequireTest: false}}
	off.explicitKeys = map[string]bool{"verification.require_test": true}
	result = MergeConfig(on(), off)
	assert.False(t, result.RequireTest(), "an explicit false must beat a broader enable")

	// Narrower layer never names the key: the broader value stands.
	silent := &Config{Verification: &VerificationConfig{Enabled: true}}
	result = MergeConfig(on(), silent)
	assert.True(t, result.RequireTest(), "a silent layer must keep the broader enable")
}

// TestRequireTestResolveCarriesFlag proves Resolve fills defaults without
// dropping the flag.
func TestRequireTestResolveCarriesFlag(t *testing.T) {
	got := (&VerificationConfig{RequireTest: true}).Resolve()
	assert.True(t, got.RequireTest)
	assert.Equal(t, DefaultVerificationRepairAttempts, got.RepairAttempts)
	assert.False(t, (&VerificationConfig{}).Resolve().RequireTest)
}

// TestRequireTestSaveLoadRoundTrip proves the flag survives the config
// save/load machinery.
func TestRequireTestSaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	require.NoError(t, os.MkdirAll(tmpDir, 0o755))
	_ = filepath.Join(tmpDir, ConfigFileName)

	cfg := NewConfig()
	cfg.Verification = &VerificationConfig{Enabled: true, RequireTest: true}
	require.NoError(t, cfg.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.True(t, loaded.RequireTest(), "an explicit enable must survive save/load")
}
