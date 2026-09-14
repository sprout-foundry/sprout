package configuration

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareConfigVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1", "2.1", 0},
		{"2.0", "2.1", -1},
		{"2.1", "2.0", 1},
		{"2.2", "2.10", -1}, // numeric, not lexicographic
		{"2.10", "2.2", 1},  // numeric, not lexicographic
		{"3.0", "2.9", 1},   // major wins
		{"0.0", "2.0", -1},
		{"2.1", "2.1.1", -1}, // longer version with extra segment
		{"", "0.0", 0},       // empty segments compare as 0
		{"2.x", "2.1", -1},   // non-numeric segment → 0
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, compareConfigVersions(tc.a, tc.b), "compare(%q, %q)", tc.a, tc.b)
	}
}

func TestMigrateConfigNewerThanBuildIsTypedError(t *testing.T) {
	raw := map[string]interface{}{"version": "2.2"}

	migrated, err := MigrateConfig(raw, "2.1")
	require.Error(t, err)

	var newer *ConfigFromNewerBuildError
	require.True(t, errors.As(err, &newer), "error should unwrap to *ConfigFromNewerBuildError, got %T", err)
	assert.Equal(t, "2.2", newer.ConfigVersion)
	assert.Equal(t, "2.1", newer.BuildVersion)
	assert.Contains(t, err.Error(), "newer than this build")
	assert.Equal(t, raw, migrated, "raw config should be returned unchanged for use as-is")
}

func TestMigrateConfigSameAndOlderVersionsUnaffected(t *testing.T) {
	// Same version → no-op, no error.
	raw := map[string]interface{}{"version": "2.1"}
	migrated, err := MigrateConfig(raw, "2.1")
	require.NoError(t, err)
	assert.Equal(t, "2.1", migrated["version"])

	// Older version still migrates through the registered chain.
	old := map[string]interface{}{"version": "2.0"}
	migrated, err = MigrateConfig(old, "2.1")
	require.NoError(t, err)
	assert.Equal(t, "2.1", migrated["version"])
}

// TestLoadWithNewerConfigVersionDoesNotSpam verifies the end-to-end path:
// an on-disk config stamped by a newer build loads successfully and the
// log note is emitted exactly once across repeated Load() calls.
func TestLoadWithNewerConfigVersionDoesNotSpam(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	configPath := filepath.Join(tmpDir, ConfigFileName)
	newerConfig := `{"version": "9.9", "last_used_provider": "ollama-local"}`
	require.NoError(t, os.WriteFile(configPath, []byte(newerConfig), 0600))

	// Clear any state left by earlier tests in this package, then capture log output.
	migrationLogged = sync.Map{}
	var buf bytes.Buffer
	originalOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(originalOut) })

	for i := 0; i < 3; i++ {
		cfg, err := Load()
		require.NoError(t, err, "Load %d should not fail on a newer config", i)
		require.NotNil(t, cfg)
		assert.Equal(t, "ollama-local", cfg.LastUsedProvider)
		assert.Equal(t, "9.9", cfg.Version, "newer config is used as-is")
	}

	out := buf.String()
	assert.Contains(t, out, "newer than this build", "should log a note about the version skew")
	count := strings.Count(out, "newer than this build")
	assert.Equal(t, 1, count, "note should appear exactly once across 3 loads, got %d", count)
}
