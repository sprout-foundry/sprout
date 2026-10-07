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

// TestOffWorkspacePathInCommandPatternArguments pins the rule that a rooted
// token whose top-level directory does not exist on this machine is treated as
// a pattern/route string, not a file target. Quoted grep patterns like
// `grep -n "/api/git/"` must not trigger an "outside the workspace root" prompt,
// while a genuine outside path with an existing top-level directory (e.g.
// /etc/hosts) must still be flagged.
func TestOffWorkspacePathInCommandPatternArguments(t *testing.T) {
	ws := "/home/dev/ws"

	// A route-style pattern argument under a nonexistent top-level dir is not a
	// path: it must not be flagged.
	patternCases := []string{
		`grep -n "/api/git/" file`,
		`grep -rn '/api/git/' internal/`,
		`rg "/api/git/"`,
		`git grep -n "/api/v1/users/"`,
	}
	for _, cmd := range patternCases {
		if offWorkspacePathInCommand(cmd, ws, nil) {
			t.Errorf("route/pattern argument must not be flagged: %q", cmd)
		}
	}

	// Genuine outside paths have an existing top-level directory and stay flagged.
	realCases := []string{
		"cat /etc/hosts",
		"cat /etc/passwd",
		"ls /usr/local/foo",
		// A ".." under a nonexistent top-level dir must NOT be excused by the
		// top-level-existence shortcut: the shell resolves it against the
		// filesystem root, so this reaches /etc/hosts.
		"cat /nonexistent/../etc/hosts",
		"cat /no-such-dir/../../etc/passwd",
	}
	for _, cmd := range realCases {
		if !offWorkspacePathInCommand(cmd, ws, nil) {
			t.Errorf("real outside path must still be flagged: %q", cmd)
		}
	}

	// Intended boundary: an outside path under a top-level tree that does not
	// exist locally is treated as a pattern and NOT flagged. This is a
	// deliberate trade-off, pinned here so a future change cannot widen it
	// silently.
	if offWorkspacePathInCommand("cat /prod/data", ws, nil) {
		t.Error("path under a nonexistent top-level dir is treated as a pattern and must not be flagged")
	}

	// And confirm the classifier escalation behaves the same way end to end.
	prompted := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": `grep -n "/api/git/" file`}, ws)
	if prompted.ShouldPrompt {
		t.Errorf("grep route pattern must not prompt, got risk=%v reason=%q", prompted.Risk, prompted.Reasoning)
	}
	outside := ClassifyToolCallWithWorkspace("shell_command", map[string]interface{}{"command": "cat /etc/hosts"}, ws)
	if !outside.ShouldPrompt || outside.Risk != SecurityCaution {
		t.Errorf("cat /etc/hosts must prompt as Caution, got risk=%v prompt=%v", outside.Risk, outside.ShouldPrompt)
	}
}

// TestTopLevelDirExists pins the existence probe used to distinguish pattern
// arguments from real paths. It must check only the top-level component.
func TestTopLevelDirExists(t *testing.T) {
	if !topLevelDirExists("/usr/bin/env") {
		t.Errorf("top-level /usr must exist")
	}
	if !topLevelDirExists("/") {
		t.Errorf("root must exist")
	}
	if topLevelDirExists("/api/git/") {
		t.Errorf("/api must not exist on this machine (pattern argument)")
	}
	if topLevelDirExists("/sprout-nonexistent-xyz/deep/path") {
		t.Errorf("nonexistent top-level dir must report false")
	}
}
