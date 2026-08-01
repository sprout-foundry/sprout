//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsHomeWorkspace verifies the home-detection predicate for the home,
// non-home, empty, and subdirectory-of-home cases. Only an exact (symlink-safe)
// match of the home directory itself should return true.
func TestIsHomeWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("cannot resolve home directory for test: %v", err)
	}
	// Canonicalize via EvalSymlinks so the comparison accounts for macOS
	// /var -> /private/var style links.
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Skipf("cannot resolve home symlinks for test: %v", err)
	}

	tmpDir := t.TempDir()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"actual home dir", resolvedHome, true},
		{"temp dir", tmpDir, false},
		{"empty string", "", false},
		{"subdirectory of home", filepath.Join(resolvedHome, "dev"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The home-subdir case only makes sense if that dir exists; skip
			// cleanly if it doesn't so the test stays robust on minimal envs.
			if tc.name == "subdirectory of home" {
				if _, statErr := os.Stat(tc.path); os.IsNotExist(statErr) {
					t.Skipf("subdirectory %q does not exist; skipping", tc.path)
				}
			}
			got := isHomeWorkspace(tc.path)
			if got != tc.want {
				t.Errorf("isHomeWorkspace(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestHomeWorkspaceConsentRoundTrip verifies the consent store degrades to
// "no consent" initially, persists on record, and reads back as consented.
// Test isolation is achieved by pointing HOME at a temp directory: resolveHomeDir
// tries os.UserHomeDir() first, which honors the $HOME env var at call time.
func TestHomeWorkspaceConsentRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if hasHomeWorkspaceConsent() {
		t.Fatal("expected no consent initially")
	}

	if err := recordHomeWorkspaceConsent(); err != nil {
		t.Fatalf("recordHomeWorkspaceConsent failed: %v", err)
	}

	if !hasHomeWorkspaceConsent() {
		t.Fatal("expected consent to be recorded after recordHomeWorkspaceConsent")
	}

	// Verify the consent file landed inside the temp home, not the real one.
	consentPath := homeConsentPath()
	if !strings.HasPrefix(consentPath, tmpHome) {
		t.Fatalf("consent path %q is not under temp home %q", consentPath, tmpHome)
	}
}

// TestSetClientWorkspaceRootRejectsHome verifies the defense-in-depth gate in
// setClientWorkspaceRoot: home is rejected without consent and accepted after
// consent is recorded. daemonRoot is set to the resolved home so the home path
// passes the isWithinWorkspace check and reaches the home gate.
func TestSetClientWorkspaceRootRejectsHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	home := resolveHomeDir()
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("resolve home symlinks: %v", err)
	}

	ws := &ReactWebServer{
		daemonRoot: resolvedHome,
	}
	clientID := "test-client"

	// Without consent: setting home must fail.
	_, err = ws.setClientWorkspaceRoot(clientID, resolvedHome)
	if err == nil {
		t.Fatal("expected error setting home workspace without consent, got nil")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("expected error mentioning home directory, got: %v", err)
	}

	// Record consent, then retry: should succeed now.
	if err := recordHomeWorkspaceConsent(); err != nil {
		t.Fatalf("recordHomeWorkspaceConsent failed: %v", err)
	}

	got, err := ws.setClientWorkspaceRoot(clientID, resolvedHome)
	if err != nil {
		t.Fatalf("expected success after consent, got error: %v", err)
	}
	// Confirm the workspace was actually set.
	if got != resolvedHome {
		t.Fatalf("expected workspace root %q, got %q", resolvedHome, got)
	}
}
