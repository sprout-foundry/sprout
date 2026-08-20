package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyToolCallWithWorkspace(t *testing.T) {
	ws := "/home/dev/ws"
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}

	safe := func(cmd string) bool {
		t.Helper()
		res := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": cmd}, ws)
		return res.Risk == SecuritySafe
	}

	cases := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"relative ls", "ls -la", true},
		{"workspace abs path", "cat /home/dev/ws/main.go", true},
		{"tmp allowed", "cp file /tmp/sprout/x.txt", true},
		{"dev null", "make build > /dev/null 2>&1", true},
		{"system binary env", "/usr/bin/env python3 script.py", true},
		{"system binary sh", "/bin/sh -c 'echo hello'", true},
		{"system binary grep", "cat data | /usr/bin/grep pattern", true},
		{"system lib ldd", "ldd /usr/lib/x86_64-linux-gnu/libc.so.6", true},
		{"home outside ws", "cat " + filepath.Join(home, "notes.txt"), false},
		{"sibling repo", "ls /home/dev/other-repo/src", false},
		{"system path", "cat /etc/passwd", false},
		{"redirect outside", "echo x > /home/dev/outside.txt", false},
		{"tilde escape", "cat ~/secrets.env", false},
		{"flag value path", "grep -f /home/dev/other/patterns.txt x", false},
		{"pipe segment outside", "cat /home/dev/ws/a | wc -l", true},
		{"pipe target outside", "cat a | tee /home/dev/other/out.txt", false},
	}
	for _, tc := range cases {
		if got := safe(tc.cmd); got != tc.safe {
			t.Errorf("%s: safe=%v want %v (cmd=%q)", tc.name, got, tc.safe, tc.cmd)
		}
	}
}

func TestClassifyToolCallWithWorkspaceEscalatesToCaution(t *testing.T) {
	ws := "/home/dev/ws"
	res := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "cat /home/dev/other-repo/main.go"}, ws)
	if res.Risk != SecurityCaution {
		t.Fatalf("expected Caution, got %v", res.Risk)
	}
	if !res.ShouldPrompt {
		t.Fatal("expected ShouldPrompt=true")
	}
	if res.RiskType != "filesystem_outside_workspace" {
		t.Fatalf("expected risk type filesystem_outside_workspace, got %s", res.RiskType)
	}
}

func TestClassifyToolCallWithWorkspaceExtraAllowed(t *testing.T) {
	ws := "/home/dev/ws"
	extra := "/home/dev/shared"
	res := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "cat /home/dev/shared/lib.go"}, ws, extra)
	if res.Risk != SecuritySafe {
		t.Fatalf("expected Safe with extraAllowed, got %v", res.Risk)
	}
	res2 := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "cat /home/dev/other-repo/main.go"}, ws, extra)
	if res2.Risk == SecuritySafe {
		t.Fatal("non-allowlisted sibling must not be Safe")
	}
}

func TestClassifyToolCallWithWorkspaceNoBaseEscalation(t *testing.T) {
	ws := "/home/dev/ws"
	// rm -rf against a sibling home path: base classifier calls it Safe
	// (the exact hole this wrapper closes) — the wrapper must escalate.
	escalated := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "rm -rf /home/dev/other"}, ws)
	if escalated.Risk != SecurityCaution {
		t.Fatalf("expected Caution escalation, got %v", escalated.Risk)
	}
	// Hard blocks stay hard blocks.
	hb := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "rm -rf /"}, ws)
	if hb.Risk != SecurityDangerous || !hb.IsHardBlock {
		t.Fatalf("hard block must remain Dangerous+IsHardBlock, got %v block=%v", hb.Risk, hb.IsHardBlock)
	}

	nonShell := ClassifyToolCallWithWorkspace("read_file", map[string]interface{}{"path": "/home/dev/other/x"}, ws)
	if nonShell.Risk != ClassifyToolCall("read_file", map[string]interface{}{"path": "/home/dev/other/x"}).Risk {
		t.Fatal("non-shell tools must return base classification")
	}
}

func TestOffWorkspacePathInCommandDotDot(t *testing.T) {
	ws := "/home/dev/ws"
	if !offWorkspacePathInCommand("cat ../../etc/hosts", ws, nil) {
		t.Fatal("../ escape must be detected")
	}
	if !offWorkspacePathInCommand("ls sub/../../other", ws, nil) {
		t.Fatal("mixed ../ escape must be detected")
	}
	if offWorkspacePathInCommand("ls ../ws-sub/../ws", ws, nil) {
		t.Fatal("path resolving back into workspace must not flag")
	}
}

func TestOffWorkspacePathRegex(t *testing.T) {
	toks := offWorkspacePathPattern.FindAllStringSubmatch("cat /etc/passwd | tee ~/x > /home/dev/ws/out", -1)
	var paths []string
	for _, m := range toks {
		paths = append(paths, m[1])
	}
	joined := paths[0] + "," + paths[1] + "," + paths[2]
	if joined != "/etc/passwd,~/x,/home/dev/ws/out" {
		t.Fatalf("unexpected tokens: %v", paths)
	}
}
