package agent

import "testing"

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
