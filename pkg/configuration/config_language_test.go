package configuration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigLanguageFieldRoundTrip proves the new `language` setting
// survives the config save/load machinery: set it, save to a
// fresh config dir, load it back, and confirm the value is preserved.
func TestConfigLanguageFieldRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	original := NewConfig()
	original.Language = "es"

	require.NoError(t, original.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "es", loaded.Language)
}

// TestConfigLanguageFieldJSONRoundTrip proves the JSON contract of the field:
// it serializes under the `language` key, is omitted when empty (omitempty),
// and unmarshals back.
func TestConfigLanguageFieldJSONRoundTrip(t *testing.T) {
	// Set: the key is present with the value.
	cfg := NewConfig()
	cfg.Language = "ru"
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var withKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withKey))
	assert.Equal(t, "ru", withKey["language"])

	// Empty: the key is omitted (omitempty).
	cfg.Language = ""
	data, err = json.Marshal(cfg)
	require.NoError(t, err)
	var withoutKey map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &withoutKey))
	assert.NotContains(t, withoutKey, "language", "an empty language must be omitted")

	// Unmarshal: the value is read back.
	var out Config
	require.NoError(t, json.Unmarshal([]byte(`{"language":"fr"}`), &out))
	assert.Equal(t, "fr", out.Language)
}

// TestMergeConfig_Language proves the merge convention for the field: a
// non-empty override wins, and an empty override keeps the base value.
func TestMergeConfig_Language(t *testing.T) {
	base := &Config{Language: "es"}

	result := MergeConfig(base, &Config{Language: "fr"})
	assert.Equal(t, "fr", result.Language, "a non-empty override wins")

	result = MergeConfig(base, &Config{})
	assert.Equal(t, "es", result.Language, "an empty override keeps the base value")

	result = MergeConfig(&Config{}, &Config{Language: "de"})
	assert.Equal(t, "de", result.Language, "an override alone is applied")
}
