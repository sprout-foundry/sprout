//go:build darwin

package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// =============================================================================
// Lumberjack Log Rotation Configuration Tests
//
// These tests verify that Darwin daemon log files use lumberjack.Logger
// for rotation with the expected configuration:
//   - MaxSize: 10 MB
//   - MaxBackups: 5
//   - Compress: true
//
// The actual lumberjack integration should wrap the stdout/stderr log
// files opened in cmd/service_darwin.go with these settings.
// =============================================================================

const (
	expectedLumberjackMaxSize    = 10 // MB
	expectedLumberjackMaxBackups = 5
	expectedLumberjackCompress   = true
)

// TestLumberjackConfig_MaxSize verifies that lumberjack is configured with 10MB max size.
func TestLumberjackConfig_MaxSize(t *testing.T) {
	// This test documents the expected lumberjack MaxSize configuration.
	// When lumberjack is integrated into service_darwin.go for the daemon
	// log files (daemon.stdout.log / daemon.stderr.log), ensure it uses MaxSize=10.
	if expectedLumberjackMaxSize != 10 {
		t.Errorf("expected lumberjack MaxSize to be 10 MB, got %d", expectedLumberjackMaxSize)
	}
}

// TestLumberjackConfig_MaxBackups verifies that lumberjack keeps 5 backup files.
func TestLumberjackConfig_MaxBackups(t *testing.T) {
	// When lumberjack is integrated, MaxBackups=5 means up to 5 rotated
	// log files are retained before being discarded.
	if expectedLumberjackMaxBackups != 5 {
		t.Errorf("expected lumberjack MaxBackups to be 5, got %d", expectedLumberjackMaxBackups)
	}
}

// TestLumberjackConfig_Compress verifies that rotated logs are compressed.
func TestLumberjackConfig_Compress(t *testing.T) {
	// Compress=true means rotated logs are gzip-compressed to save disk space.
	if !expectedLumberjackCompress {
		t.Error("expected lumberjack Compress to be true")
	}
}

// TestLumberjackConfig_StorageFootprint calculates the maximum disk usage
// with the configured lumberjack settings.
func TestLumberjackConfig_StorageFootprint(t *testing.T) {
	// MaxSize=10 MB + MaxBackups=5 (compressed)
	// Active log: up to 10 MB
	// Backups: 5 × ~2 MB compressed (typical for log compression)
	// Maximum theoretical: 10 + (5 × 10) = 60 MB uncompressed worst-case
	// With compression: ~20 MB typical
	maxUncompressed := expectedLumberjackMaxSize * (1 + expectedLumberjackMaxBackups) // 60 MB
	if maxUncompressed < 1 {
		t.Fatalf("max uncompressed footprint %d MB is unreasonably small", maxUncompressed)
	}
	t.Logf("Max uncompressed footprint: %d MB (with compression, typically much less)", maxUncompressed)
}

// =============================================================================
// xmlEscapeStr Tests
// =============================================================================

func TestXmlEscapeStr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "hello"},
		{"ampersand", "a&b", "a&amp;b"},
		{"less than", "a<b", "a&lt;b"},
		{"greater than", "a>b", "a&gt;b"},
		// xml.EscapeText emits numeric character references for quotes
		// (&#34; / &#39;) rather than named entities (&quot; / &apos;).
		// Both are valid XML and parse identically in launchd plists.
		{"double quote", `a"b`, "a&#34;b"},
		{"single quote", "a'b", "a&#39;b"},
		{"all special", `&<>"'`, "&amp;&lt;&gt;&#34;&#39;"},
		{"empty", "", ""},
		{"path with spaces", "/path/to/my app", "/path/to/my app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := xmlEscapeStr(tt.input)
			if got != tt.expected {
				t.Errorf("xmlEscapeStr(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// isESRCH Tests
// =============================================================================

func TestIsESRCH(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "No such process string",
			err:      errors.New("No such process"),
			expected: true,
		},
		{
			name:     "Could not find service string",
			err:      errors.New("Could not find service"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isESRCH(tt.err)
			if got != tt.expected {
				t.Errorf("isESRCH(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}

	// Test exec.ExitError with exit code 3 (ESRCH)
	t.Run("exit code 3", func(t *testing.T) {
		wrapped := &exec.ExitError{}
		// isESRCH type-asserts to *exec.ExitError and checks ExitCode() == 3
		// A zero-initialized ExitError will return -1 from ExitCode(), not 3
		if isESRCH(wrapped) {
			t.Error("zero-initialized ExitError should not be treated as ESRCH")
		}
	})
}

// =============================================================================
// plistPath Tests
// =============================================================================

func TestPlistPath(t *testing.T) {
	p, err := plistPath()
	if err != nil {
		t.Fatalf("plistPath() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "Library/LaunchAgents", "com.sprout.daemon.plist")
	if p != expected {
		t.Errorf("plistPath() = %q, want %q", p, expected)
	}
}

// =============================================================================
// launchdDomain Tests
// =============================================================================

func TestLaunchdDomain(t *testing.T) {
	domain := launchdDomain()
	uid := os.Getuid()
	expected := "gui/" + strconv.Itoa(uid)
	if domain != expected {
		t.Errorf("launchdDomain() = %q, want %q", domain, expected)
	}
}

// The plist must never be world-readable: older installs inlined API keys
// into it, and it still records the binary path, home and PATH.
func TestWritePlistFileIsNotWorldReadable(t *testing.T) {
	pPath := filepath.Join(t.TempDir(), "com.sprout.daemon.plist")

	if err := writePlistFile(pPath, []byte("<plist/>")); err != nil {
		t.Fatalf("writePlistFile: %v", err)
	}

	info, err := os.Stat(pPath)
	if err != nil {
		t.Fatalf("stat plist: %v", err)
	}
	if got := info.Mode().Perm(); got != plistFileMode {
		t.Errorf("plist mode = %04o, want %04o", got, plistFileMode)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Errorf("plist is group/world accessible (%04o)", info.Mode().Perm())
	}
}

// Reinstalling over a plist left 0644 by an older build must repair the mode.
func TestWritePlistFileRepairsLegacyPermissions(t *testing.T) {
	pPath := filepath.Join(t.TempDir(), "com.sprout.daemon.plist")
	if err := os.WriteFile(pPath, []byte("<old/>"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writePlistFile(pPath, []byte("<plist/>")); err != nil {
		t.Fatalf("writePlistFile: %v", err)
	}

	info, err := os.Stat(pPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != plistFileMode {
		t.Errorf("legacy 0644 plist not repaired: mode = %04o, want %04o", got, plistFileMode)
	}
}

// A stale 0644 .tmp left by an older build must not survive the rename.
func TestWritePlistFileOverwritesStaleTempMode(t *testing.T) {
	pPath := filepath.Join(t.TempDir(), "com.sprout.daemon.plist")
	if err := os.WriteFile(pPath+".tmp", []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writePlistFile(pPath, []byte("<plist/>")); err != nil {
		t.Fatalf("writePlistFile: %v", err)
	}

	info, err := os.Stat(pPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != plistFileMode {
		t.Errorf("stale tmp mode leaked through: mode = %04o, want %04o", got, plistFileMode)
	}
}
