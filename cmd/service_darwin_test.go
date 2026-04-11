//go:build darwin

package cmd

import (
	"strings"
	"testing"
)

func TestGenerateLaunchdPlist(t *testing.T) {
	data, err := generateLaunchdPlist("/usr/local/bin/ledit", "/Users/testuser")
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error: %v", err)
	}

	xml := string(data)

	tests := []struct {
		name     string
		contains string
	}{
		{"has xml declaration", "<?xml version="},
		{"has plist root", "<plist version="},
		{"has label key", "<key>Label</key>"},
		{"has label value", "com.ledit.daemon"},
		{"has program arguments key", "<key>ProgramArguments</key>"},
		{"has binary path", "/usr/local/bin/ledit"},
		{"has agent arg", ">agent<"},
		{"has daemon flag", ">-d<"},
		{"has no-connection-check", ">--no-connection-check<"},
		{"has working directory key", "<key>WorkingDirectory</key>"},
		{"has run at load", "<true/>"},
		{"has keep alive", "<key>KeepAlive</key>"},
		{"has throttle interval", "<key>ThrottleInterval</key>"},
		{"has stdout path key", "<key>StandardOutPath</key>"},
		{"has stderr path key", "<key>StandardErrorPath</key>"},
		{"has env vars key", "<key>EnvironmentVariables</key>"},
		{"has dict open", "<dict>"},
		{"has dict close", "</dict>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(xml, tt.contains) {
				t.Errorf("plist missing %q\n%s", tt.contains, xml)
			}
		})
	}
}

func TestPlistEnvironmentVariables(t *testing.T) {
	data, err := generateLaunchdPlist("/opt/ledit/bin/ledit", "/home/alice")
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error: %v", err)
	}

	xml := string(data)

	tests := []struct {
		name     string
		contains string
	}{
		{"has LEDIT_SERVICE key", "<key>LEDIT_SERVICE</key>"},
		{"has LEDIT_SERVICE value 1", ">1<"},
		{"has HOME key in env", "<key>HOME</key>"},
		{"has HOME value", ">home/alice<"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(xml, tt.contains) {
				t.Errorf("plist missing env var entry %q\n%s", tt.contains, xml)
			}
		})
	}
}

func TestPlistLogPaths(t *testing.T) {
	data, err := generateLaunchdPlist("/usr/bin/ledit", "/Users/bob")
	if err != nil {
		t.Fatalf("generateLaunchdPlist() error: %v", err)
	}

	xml := string(data)

	expectedPaths := []string{
		"/Users/bob/.ledit/logs/daemon.stdout.log",
		"/Users/bob/.ledit/logs/daemon.stderr.log",
	}

	for _, path := range expectedPaths {
		if !strings.Contains(xml, path) {
			t.Errorf("plist missing log path %q\n%s", path, xml)
		}
	}

	// Verify log directory reference.
	if !strings.Contains(xml, ".ledit/logs/") {
		t.Error("plist missing .ledit/logs/ directory reference")
	}
}

func TestLaunchdDomain(t *testing.T) {
	domain := launchdDomain()
	if !strings.HasPrefix(domain, "gui/") {
		t.Errorf("launchdDomain() = %q, want prefix 'gui/'", domain)
	}
	if domain == "gui/" {
		t.Error("launchdDomain() missing uid")
	}
}
