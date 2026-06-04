package agent

import (
	"os"
	"path/filepath"
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
