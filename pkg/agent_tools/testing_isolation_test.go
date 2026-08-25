package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain isolates all tools-package tests from the real workspace config.
//
// Without this, tests using NewManagerWithConfig (configDir="") that trigger
// UpdateConfig/Save write to wherever SPROUT_CONFIG points — which inside a
// git repo is the real .sprout/config.json, corrupting the user's workspace
// config with test fixtures.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sprout-tools-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	os.Setenv("SPROUT_CONFIG", filepath.Join(tmpDir, ".config", "sprout"))
	os.Exit(m.Run())
}
