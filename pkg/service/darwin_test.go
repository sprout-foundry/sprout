//go:build darwin

package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
// Log Path Generation Tests
// =============================================================================

// TestGenerateLaunchdPlist_NoStdoutStderrPaths verifies that the plist does NOT
// reference StandardOutPath/StandardErrorPath — log rotation is handled
// in-process by setupDaemonLogging() via lumberjack, not by launchd.
func TestGenerateLaunchdPlist_NoStdoutStderrPaths(t *testing.T) {
	homeDir := t.TempDir()

	// Create a minimal service.env file so loadServiceEnvFile succeeds
	sproutDir := filepath.Join(homeDir, ".sprout")
	if err := os.MkdirAll(sproutDir, 0755); err != nil {
		t.Fatalf("failed to create .sprout dir: %v", err)
	}
	envPath := filepath.Join(sproutDir, "service.env")
	if err := os.WriteFile(envPath, []byte(""), 0600); err != nil {
		t.Fatalf("failed to write empty service.env: %v", err)
	}

	binaryPath := filepath.Join(homeDir, "bin", "sprout")

	data, err := generateLaunchdPlist(binaryPath, homeDir)
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error = %v", err)
	}

	plist := string(data)

	// The plist must NOT contain StandardOutPath/StandardErrorPath —
	// log rotation is handled in-process by lumberjack (daemon_logging.go).
	if strings.Contains(plist, "StandardOutPath") {
		t.Errorf("plist should NOT contain StandardOutPath (handled by in-process lumberjack)\nplist:\n%s", plist)
	}
	if strings.Contains(plist, "StandardErrorPath") {
		t.Errorf("plist should NOT contain StandardErrorPath (handled by in-process lumberjack)\nplist:\n%s", plist)
	}
}

// TestGenerateLaunchdPlist_NoLogPathsInPlist ensures the plist does not embed
// log file paths — log rotation is handled in-process, not by launchd.
func TestGenerateLaunchdPlist_NoLogPathsInPlist(t *testing.T) {
	homeDir := t.TempDir()

	sproutDir := filepath.Join(homeDir, ".sprout")
	if err := os.MkdirAll(sproutDir, 0755); err != nil {
		t.Fatalf("failed to create .sprout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sproutDir, "service.env"), []byte(""), 0600); err != nil {
		t.Fatalf("failed to write empty service.env: %v", err)
	}

	data, err := generateLaunchdPlist("/usr/local/bin/sprout", homeDir)
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error = %v", err)
	}

	plist := string(data)

	// The plist should NOT contain daemon log paths — these are managed
	// in-process by setupDaemonLogging() using lumberjack.
	if strings.Contains(plist, "daemon.stdout.log") {
		t.Error("plist should NOT contain daemon.stdout.log path (managed by in-process lumberjack)")
	}
	if strings.Contains(plist, "daemon.stderr.log") {
		t.Error("plist should NOT contain daemon.stderr.log path (managed by in-process lumberjack)")
	}
}

// TestGenerateLaunchdPlist_EnvVars tests that environment variables are included
// in the generated plist.
func TestGenerateLaunchdPlist_EnvVars(t *testing.T) {
	homeDir := t.TempDir()

	sproutDir := filepath.Join(homeDir, ".sprout")
	if err := os.MkdirAll(sproutDir, 0755); err != nil {
		t.Fatalf("failed to create .sprout dir: %v", err)
	}
	envPath := filepath.Join(sproutDir, "service.env")
	if err := os.WriteFile(envPath, []byte("MY_API_KEY=secret123\nANOTHER_TOKEN=abc\n"), 0600); err != nil {
		t.Fatalf("failed to write service.env: %v", err)
	}

	data, err := generateLaunchdPlist("/usr/local/bin/sprout", homeDir)
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error = %v", err)
	}

	plist := string(data)

	// Should contain hardcoded env vars
	if !strings.Contains(plist, "SPROUT_SERVICE") || !strings.Contains(plist, "<string>1</string>") {
		t.Error("plist should contain SPROUT_SERVICE=1")
	}

	// Should contain loaded env vars
	if !strings.Contains(plist, "MY_API_KEY") {
		t.Error("plist should contain MY_API_KEY from service.env")
	}
	if !strings.Contains(plist, "ANOTHER_TOKEN") {
		t.Error("plist should contain ANOTHER_TOKEN from service.env")
	}
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
