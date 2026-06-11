package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withAutomateDir creates an automate/ dir under a temp cwd, writes the named
// workflow JSON, switches to that cwd for the duration of the test, and
// returns the cwd. Cleanup restores the previous cwd automatically via
// t.Chdir (Go 1.24+).
func withAutomateDir(t *testing.T, files map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	auto := filepath.Join(tmp, "automate")
	if err := os.MkdirAll(auto, 0755); err != nil {
		t.Fatalf("mkdir automate: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(auto, name), []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Chdir(tmp)
	return tmp
}

func TestWorkflowRequiresApproval_DefaultsToTrue(t *testing.T) {
	withAutomateDir(t, map[string]string{
		"check.json": `{"initial":{"prompt":"hi"}}`,
	})
	if !workflowRequiresApproval("check.json") {
		t.Fatalf("workflow without requires_approval field should require approval")
	}
}

func TestWorkflowRequiresApproval_FalseSkipsApproval(t *testing.T) {
	withAutomateDir(t, map[string]string{
		"check.json": `{"requires_approval": false, "initial":{"prompt":"hi"}}`,
	})
	if workflowRequiresApproval("check.json") {
		t.Fatalf("workflow with requires_approval=false should NOT require approval")
	}
	// Bare name (no .json) should resolve the same workflow.
	if workflowRequiresApproval("check") {
		t.Fatalf("bare name lookup should resolve and return false")
	}
}

func TestWorkflowRequiresApproval_FailsSafe(t *testing.T) {
	withAutomateDir(t, map[string]string{
		"valid.json": `{"requires_approval": false, "initial":{"prompt":"hi"}}`,
	})
	// Unknown workflow falls through to requiring approval (fail-safe).
	if !workflowRequiresApproval("nonexistent.json") {
		t.Fatalf("unresolvable workflow must default to requiring approval")
	}
}

func TestWorkflowRequiresApproval_MalformedJsonFailsSafe(t *testing.T) {
	withAutomateDir(t, map[string]string{
		"broken.json": `{"requires_approval": false,`,
	})
	// Malformed JSON still has the regex-valid filename, but Summarize will
	// fail. Must fall through to requiring approval.
	if !workflowRequiresApproval("broken.json") {
		t.Fatalf("malformed JSON must default to requiring approval")
	}
}

func TestWorkflowApprovalCache_MarkAndCheck(t *testing.T) {
	a := &Agent{}

	if a.IsWorkflowApprovedInSession("foo.json") {
		t.Fatalf("fresh agent should not have any pre-approved workflows")
	}

	a.MarkWorkflowApprovedInSession("foo.json")
	if !a.IsWorkflowApprovedInSession("foo.json") {
		t.Fatalf("workflow should be approved after MarkWorkflowApprovedInSession")
	}
}

func TestWorkflowApprovalCache_NormalizesKey(t *testing.T) {
	a := &Agent{}

	// Mark with a relative path and look up with bare basename.
	a.MarkWorkflowApprovedInSession("automate/foo.json")
	if !a.IsWorkflowApprovedInSession("foo.json") {
		t.Fatalf("basename lookup should match path-style mark")
	}

	// Case-insensitive match.
	if !a.IsWorkflowApprovedInSession("FOO.json") {
		t.Fatalf("approval cache should be case-insensitive")
	}

	// Approving the bare name should also satisfy the .json form
	// (and vice versa), since ResolvePath treats them as the same file.
	a2 := &Agent{}
	a2.MarkWorkflowApprovedInSession("bar")
	if !a2.IsWorkflowApprovedInSession("bar.json") {
		t.Fatalf("approving %q should satisfy %q lookup", "bar", "bar.json")
	}

	a3 := &Agent{}
	a3.MarkWorkflowApprovedInSession("baz.json")
	if !a3.IsWorkflowApprovedInSession("baz") {
		t.Fatalf("approving %q should satisfy %q lookup", "baz.json", "baz")
	}
}

func TestWorkflowApprovalCache_EmptyKeyIsNoOp(t *testing.T) {
	a := &Agent{}
	a.MarkWorkflowApprovedInSession("")
	a.MarkWorkflowApprovedInSession("   ")
	if a.IsWorkflowApprovedInSession("") {
		t.Fatalf("empty key should not match")
	}
}

func TestWorkflowApprovalCache_NilAgentSafe(t *testing.T) {
	var a *Agent
	if a.IsWorkflowApprovedInSession("foo.json") {
		t.Fatalf("nil agent should report not approved")
	}
	a.MarkWorkflowApprovedInSession("foo.json") // must not panic
}

func TestWorkflowApprovalCache_DifferentWorkflowsIndependent(t *testing.T) {
	a := &Agent{}
	a.MarkWorkflowApprovedInSession("foo.json")
	if a.IsWorkflowApprovedInSession("bar.json") {
		t.Fatalf("approving foo should not approve bar")
	}
}

func TestReadOutputTail_FileMissing(t *testing.T) {
	got := readOutputTail("/tmp/sprout-nonexistent-output-file-12345.txt", 2048)
	if got != "" {
		t.Fatalf("expected empty string for missing file, got %q", got)
	}
}

func TestReadOutputTail_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	got := readOutputTail(path, 2048)
	if got != "" {
		t.Fatalf("expected empty string for empty file, got %q", got)
	}
}

func TestReadOutputTail_SmallFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "small.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got := readOutputTail(path, 2048)
	if got != content {
		t.Fatalf("expected full content %q, got %q", content, got)
	}
}

func TestReadOutputTail_LargeFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "large.txt")

	// Create content larger than 2KB: 3KB of repeating text
	content := strings.Repeat("A", 1024) + strings.Repeat("B", 1024) + strings.Repeat("C", 1024)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := readOutputTail(path, 2048)
	if len(got) != 2048 {
		t.Fatalf("expected 2048 bytes, got %d", len(got))
	}

	// Should be the last 2048 bytes: 1024 B's + 1024 C's
	expected := strings.Repeat("B", 1024) + strings.Repeat("C", 1024)
	if got != expected {
		t.Fatalf("tail mismatch: got starts with %q", got[:40])
	}
}

func TestReadOutputTail_PartialRead(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "partial.txt")

	// Write exactly 500 bytes of content
	content := strings.Repeat("X", 500)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Request 2048 bytes but file is only 500
	got := readOutputTail(path, 2048)
	if got != content {
		t.Fatalf("expected full 500 bytes, got %d bytes", len(got))
	}

	// Request only 100 bytes — should get last 100
	gotSmall := readOutputTail(path, 100)
	if len(gotSmall) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(gotSmall))
	}
	if gotSmall != strings.Repeat("X", 100) {
		t.Fatalf("expected last 100 X's, got %q", gotSmall)
	}
}

func TestReadOutputTail_Directory(t *testing.T) {
	tmp := t.TempDir()
	got := readOutputTail(tmp, 2048)
	if got != "" {
		t.Fatalf("expected empty string for directory, got %q", got)
	}
}

func TestReadOutputTail_StripsControlChars(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "control.txt")

	// Mix of printable, newline, tab, and control characters
	content := "line1\nline2\tval\x00null\x1besc\x07bell\nline3\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := readOutputTail(path, 2048)

	// Should contain printable chars, newline, tab — but NOT null/esc/bell
	if strings.Contains(got, "\x00") {
		t.Fatalf("should not contain null byte")
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("should not contain ESC byte")
	}
	if strings.Contains(got, "\x07") {
		t.Fatalf("should not contain BEL byte")
	}
	if !strings.Contains(got, "line1") {
		t.Fatalf("should contain printable text 'line1'")
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("should contain newlines")
	}
	if !strings.Contains(got, "\t") {
		t.Fatalf("should contain tabs")
	}
	// Verify the printable parts survived
	if !strings.Contains(got, "line2") || !strings.Contains(got, "line3") {
		t.Fatalf("should contain 'line2' and 'line3', got %q", got)
	}
}
