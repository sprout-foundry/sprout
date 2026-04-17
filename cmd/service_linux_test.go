//go:build linux

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	tests := []struct {
		name       string
		binaryPath string
		homeDir    string
		wantErr    bool
	}{
		{"valid paths", "/usr/local/bin/ledit", "/home/alice", false},
		{"empty binary", "", "/home/alice", true},
		{"empty home", "/usr/local/bin/ledit", "", true},
		{"both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := generateSystemdUnit(tt.binaryPath, tt.homeDir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("generateSystemdUnit() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			s := string(out)

			// Required section headers
			for _, section := range []string{"[Unit]", "[Service]", "[Install]"} {
				if !strings.Contains(s, section) {
					t.Errorf("missing section %q", section)
				}
			}

			// Key fields in [Unit]
			for _, field := range []string{
				"Description=ledit daemon - AI coding assistant web UI",
				"After=default.target",
			} {
				if !strings.Contains(s, field) {
					t.Errorf("missing Unit field %q", field)
				}
			}

			// Key fields in [Service]
			for _, field := range []string{
				"Type=simple",
				"Restart=on-failure",
				"RestartSec=5",
				"StandardOutput=journal",
				"StandardError=journal",
				"WantedBy=default.target",
			} {
				if !strings.Contains(s, field) {
					t.Errorf("missing field %q", field)
				}
			}

			// ExecStart uses unquoted binary path (no special chars)
			wantExec := fmt.Sprintf("ExecStart=%s agent -d --no-connection-check", tt.binaryPath)
			if !strings.Contains(s, wantExec) {
				t.Errorf("ExecStart mismatch\ngot:  %s\nwant: %s", extractLine(s, "ExecStart="), wantExec)
			}

			// WorkingDirectory uses unquoted path (systemd requires literal paths)
			wantWD := "WorkingDirectory=" + tt.homeDir
			if !strings.Contains(s, wantWD) {
				t.Errorf("WorkingDirectory mismatch\ngot:  %s\nwant: %s", extractLine(s, "WorkingDirectory="), wantWD)
			}

			// Environment lines
			if !strings.Contains(s, "Environment=LEDIT_SERVICE=1") {
				t.Error("missing Environment=LEDIT_SERVICE=1")
			}
			wantHome := "Environment=HOME=" + tt.homeDir
			if !strings.Contains(s, wantHome) {
				t.Errorf("Environment HOME mismatch\ngot:  %s\nwant: %s", extractLine(s, "Environment=HOME="), wantHome)
			}
		})
	}
}

func extractLine(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return "<not found>"
}

func TestSystemdUnitPaths(t *testing.T) {
	t.Run("paths are properly quoted for systemd", func(t *testing.T) {
		binary := "/opt/ledit/bin/ledit"
		home := "/home/testuser"
		out, err := generateSystemdUnit(binary, home)
		if err != nil {
			t.Fatal(err)
		}

		s := string(out)

		// Paths without special chars are unquoted
		expectedExec := fmt.Sprintf("ExecStart=%s agent", binary)
		if !strings.Contains(s, expectedExec) {
			t.Errorf("ExecStart should contain unquoted binary\ngot:  %s", extractLine(s, "ExecStart="))
		}

		// WorkingDirectory and Environment HOME use unquoted literal paths
		wdCount := strings.Count(s, "WorkingDirectory="+home)
		if wdCount != 1 {
			t.Errorf("expected 1 WorkingDirectory=%s, got %d", home, wdCount)
		}

		homeEnvCount := strings.Count(s, "Environment=HOME="+home)
		if homeEnvCount != 1 {
			t.Errorf("expected 1 Environment=HOME=%s, got %d", home, homeEnvCount)
		}
	})

	t.Run("paths with spaces are properly handled", func(t *testing.T) {
		binary := "/usr/bin/ledit"
		home := "/home/user with spaces"
		out, err := generateSystemdUnit(binary, home)
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)

		// WorkingDirectory and HOME use unquoted literal paths even with spaces
		// (systemd treats quotes as part of the path value)
		if !strings.Contains(s, "WorkingDirectory="+home) {
			t.Errorf("WorkingDirectory should use unquoted path\ngot:\n%s", s)
		}
		if !strings.Contains(s, "Environment=HOME="+home) {
			t.Errorf("Environment HOME should use unquoted path\ngot:\n%s", s)
		}

		// ExecStart should have the binary unquoted (no special chars)
		if !strings.Contains(s, binary+" agent") {
			t.Errorf("ExecStart should have unquoted binary path\ngot:\n%s", s)
		}
	})
}
