package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestManager_Isolation(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Manager was created successfully.
	require.NotNil(t, mgr)

	// Config comes from the temp directory, not the real user config.
	cfg := mgr.GetConfig()
	require.NotNil(t, cfg)

	// LastUsedProvider starts empty in isolated test configs: the helper
	// deliberately does NOT preload "test" any more — that string used
	// to leak into the user's real config when a test misbehaved. Tests
	// that need a specific provider must set it on the returned mgr.
	assert.Equal(t, "", cfg.LastUsedProvider,
		"isolated test config should start with empty LastUsedProvider")

	// Mutations via UpdateConfigNoSave are visible within the same manager
	// but DO NOT leak to the real config because SPROUT_CONFIG points at the
	// temp dir.
	require.NoError(t, mgr.UpdateConfigNoSave(func(c *Config) error {
		c.LastUsedProvider = "openai"
		return nil
	}))
	assert.Equal(t, "openai", mgr.GetConfig().LastUsedProvider)
}

func TestNewTestManager_DoesNotTouchRealConfig(t *testing.T) {
	// Capture the real config dir before the test.
	realCfgDir, err := GetConfigDir()
	if err != nil {
		t.Skipf("cannot determine real config dir: %v", err)
	}
	realConfigPath := filepath.Join(realCfgDir, ConfigFileName)

	// Snapshot the real config (it may not exist and that's fine).
	realBefore, _ := os.ReadFile(realConfigPath)

	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Mutate the isolated config in a way we can detect.
	require.NoError(t, mgr.UpdateConfig(func(c *Config) error {
		c.LastUsedProvider = "zzz-isolated-test-marker-zzz"
		return nil
	}))

	// Re-read the real config — it must be unchanged.
	realAfter, _ := os.ReadFile(realConfigPath)
	assert.Equal(t, string(realBefore), string(realAfter),
		"test must not modify the real user config file")
}

func TestNewTestManager_DoesNotCreateFilesOutsideTempDir(t *testing.T) {
	// After cleanup the temp dir is removed by t.TempDir(), but no files
	// should have been created in the real config location.
	realCfgDir, err := GetConfigDir()
	if err != nil {
		t.Skipf("cannot determine real config dir: %v", err)
	}

	// List files in real config dir before.
	before := listDir(t, realCfgDir)

	_, cleanup := NewTestManager(t)
	cleanup()

	after := listDir(t, realCfgDir)
	assert.Equal(t, before, after,
		"test must not create new files in the real config directory")
}

func listDir(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	m := make(map[string]bool, len(entries))
	for _, e := range entries {
		m[e.Name()] = true
	}
	return m
}
