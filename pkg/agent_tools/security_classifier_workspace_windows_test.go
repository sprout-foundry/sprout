//go:build windows

package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyToolCallWithWorkspaceWindowsPaths(t *testing.T) {
	ws := `C:\Users\dev\ws`
	cases := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"backslash in ws", `cat C:\Users\dev\ws\main.go`, true},
		{"forward slash in ws", `cat C:/Users/dev/ws/main.go`, true},
		{"case-insensitive ws", `cat c:\users\DEV\WS\main.go`, true},
		{"sibling repo", `type C:\Users\dev\other\main.go`, false},
		{"prefix sibling", `type C:\Users\dev\ws2\main.go`, false},
		{"system dir", `type C:\Windows\win.ini`, false},
		{"other drive", `dir D:\data`, false},
		{"unc share", `type \\server\share\secret.txt`, false},
		{"tilde backslash", `cat ~\secrets.env`, false},
		{"dotdot backslash", `type ..\..\other\x`, false},
		{"os temp dir", "cat " + filepath.Join(os.TempDir(), "x.txt"), true},
	}
	for _, tc := range cases {
		res := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": tc.cmd}, ws)
		if got := res.Risk == SecuritySafe; got != tc.safe {
			t.Errorf("%s: safe=%v want %v (cmd=%q)", tc.name, got, tc.safe, tc.cmd)
		}
	}
}

func TestIsScreenshotPathAllowedWindowsRooted(t *testing.T) {
	for _, p := range []string{`C:\Windows\evil.png`, `\Windows\evil.png`, `D:\evil.png`} {
		if isScreenshotPathAllowed(filepath.Clean(p)) {
			t.Errorf("isScreenshotPathAllowed(%q) = true, want false", p)
		}
	}
	if !isScreenshotPathAllowed(filepath.Join(os.TempDir(), "sprout", "shot.png")) {
		t.Error("os temp sprout dir must be allowed")
	}
}
