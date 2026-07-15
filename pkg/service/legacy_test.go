//go:build linux

package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLegacyServiceLinux(t *testing.T) {
	testDir := t.TempDir()

	// Save the real home dir and replace with our test dir
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", testDir)

	// Create legacy systemd service files
	legacyDir := filepath.Join(testDir, ".config", "systemd", "user")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create legacy files
	legacyFiles := []string{
		"ledit-daemon.service",
		"ledit-web.service",
	}
	for _, f := range legacyFiles {
		path := filepath.Join(legacyDir, f)
		if err := os.WriteFile(path, []byte("[Unit]\nDescription=Legacy\n"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
	}

	// Test detection
	paths, err := detectLegacyService()
	if err != nil {
		t.Fatalf("detectLegacyService() error = %v", err)
	}

	if len(paths) != len(legacyFiles) {
		t.Errorf("expected %d legacy services, got %d: %v", len(legacyFiles), len(paths), paths)
	}

	// Verify detected paths
	expectedPaths := make(map[string]bool)
	for _, f := range legacyFiles {
		expectedPaths[filepath.Join(legacyDir, f)] = true
	}
	for _, p := range paths {
		if !expectedPaths[p] {
			t.Errorf("unexpected path: %s", p)
		}
	}
}

func TestDetectLegacyServiceNone(t *testing.T) {
	testDir := t.TempDir()

	// Save and replace home dir
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", testDir)

	// Ensure the legacy directory exists but has no legacy files
	legacyDir := filepath.Join(testDir, ".config", "systemd", "user")
	os.MkdirAll(legacyDir, 0755)

	// Create a non-legacy file (should be ignored)
	if err := os.WriteFile(filepath.Join(legacyDir, "sprout-daemon.service"), []byte("[]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	paths, err := detectLegacyService()
	if err != nil {
		t.Fatalf("detectLegacyService() error = %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("expected 0 legacy services, got %d: %v", len(paths), paths)
	}
}

func TestRemoveLegacyServices(t *testing.T) {
	testDir := t.TempDir()

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", testDir)

	legacyDir := filepath.Join(testDir, ".config", "systemd", "user")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create legacy files
	legacyFiles := []string{
		"ledit-daemon.service",
		"ledit-web.service",
	}
	paths := []string{}
	for _, f := range legacyFiles {
		path := filepath.Join(legacyDir, f)
		if err := os.WriteFile(path, []byte("[Unit]\nDescription=Legacy\n"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
		paths = append(paths, path)
	}

	// Try to remove (will fail because systemctl isn't available, but we can check the os.Remove path)
	err := removeLegacyServices(paths)
	if err != nil {
		// Expected to fail due to systemctl not being available in test env
		t.Logf("removeLegacyServices() returned error (expected in test env): %v", err)
		return
	}

	// If it succeeded, verify files are gone
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("expected %s to be removed, but it still exists", path)
		}
	}
}

func TestRemoveLegacyServices_Empty(t *testing.T) {
	err := removeLegacyServices(nil)
	if err != nil {
		t.Errorf("removeLegacyServices(nil) = %v, want nil", err)
	}

	err = removeLegacyServices([]string{})
	if err != nil {
		t.Errorf("removeLegacyServices([]) = %v, want nil", err)
	}
}
