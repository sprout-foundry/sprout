package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/internal/testgit"
)

// TestMain isolates all tools-package tests from the real workspace config.
//
// Without this, tests using NewManagerWithConfig (configDir="") that trigger
// UpdateConfig/Save write to wherever SPROUT_CONFIG points — which inside a
// git repo is the real .sprout/config.json, corrupting the user's workspace
// config with test fixtures.
func TestMain(m *testing.M) {
	// The git/PR handlers under test exec real git subprocesses; redirect
	// their config so they can never read or write the developer's
	// real ~/.gitconfig.
	testgit.Configure()
	tmpDir, err := os.MkdirTemp("", "sprout-tools-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	os.Setenv("SPROUT_CONFIG", filepath.Join(tmpDir, ".config", "sprout"))
	os.Exit(m.Run())
}
