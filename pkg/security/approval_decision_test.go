package security

import "testing"

func TestApprovalDecisionString(t *testing.T) {
	tests := []struct {
		d    ApprovalDecision
		want string
	}{
		{ApprovalDeny, "deny"},
		{ApprovalApproveOnce, "approve_once"},
		{ApprovalApproveAlways, "approve_always"},
		{ApprovalElevate, "elevate"},
		{ApprovalDecision(999), "deny"}, // unknown → safe fallback
	}
	for _, tc := range tests {
		if got := tc.d.String(); got != tc.want {
			t.Errorf("(%d).String() = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestApprovalDecisionFromString(t *testing.T) {
	tests := []struct {
		in   string
		want ApprovalDecision
	}{
		{"approve_once", ApprovalApproveOnce},
		{"approve", ApprovalApproveOnce},
		{"yes", ApprovalApproveOnce},
		{"true", ApprovalApproveOnce},
		{"approve_always", ApprovalApproveAlways},
		{"always", ApprovalApproveAlways},
		{"elevate", ApprovalElevate},
		{"deny", ApprovalDeny},
		{"no", ApprovalDeny},
		{"", ApprovalDeny},
		{"unknown-action", ApprovalDeny},
	}
	for _, tc := range tests {
		if got := ApprovalDecisionFromString(tc.in); got != tc.want {
			t.Errorf("FromString(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestApprovalDecisionApproved(t *testing.T) {
	tests := []struct {
		d    ApprovalDecision
		want bool
	}{
		{ApprovalDeny, false},
		{ApprovalApproveOnce, true},
		{ApprovalApproveAlways, true},
		{ApprovalElevate, true},
	}
	for _, tc := range tests {
		if got := tc.d.Approved(); got != tc.want {
			t.Errorf("(%v).Approved() = %v, want %v", tc.d, got, tc.want)
		}
	}
}

// TestRequestApprovalDecisionNilBus confirms that when there's no event
// bus to dispatch to, RequestApprovalDecision returns ApprovalDeny for
// tool requests (reject-for-safety semantics preserved from the bool API).
func TestRequestApprovalDecisionNilBus(t *testing.T) {
	mgr := NewApprovalManager()
	decision := mgr.RequestApprovalDecision(nil, ApprovalRequest{
		Kind:     ApprovalKindTool,
		ToolName: "shell_command",
	})
	if decision != ApprovalDeny {
		t.Errorf("nil bus + tool kind: got %v, want ApprovalDeny", decision)
	}
	if decision.Approved() {
		t.Error("nil bus + tool kind: must not be approved")
	}
}

// TestRespondToApprovalLegacyBoolPath confirms the existing bool wrapper
// continues to drain pending requests after the channel type change.
func TestRespondToApprovalLegacyBoolPath(t *testing.T) {
	mgr := NewApprovalManager()
	mgr.SetTimeout(1) // unused; we directly seed pending

	requestID := "test-req-1"
	ch := make(chan ApprovalDecision, 1)
	mgr.mu.Lock()
	mgr.pending[requestID] = ch
	mgr.mu.Unlock()

	if !mgr.RespondToApproval(requestID, true) {
		t.Fatal("RespondToApproval should return true for known request")
	}
	got := <-ch
	if got != ApprovalApproveOnce {
		t.Errorf("bool true should map to ApprovalApproveOnce, got %v", got)
	}

	// Second one — false should map to ApprovalDeny
	mgr.mu.Lock()
	mgr.pending[requestID] = ch
	mgr.mu.Unlock()
	if !mgr.RespondToApproval(requestID, false) {
		t.Fatal("RespondToApproval should return true for known request")
	}
	if got := <-ch; got != ApprovalDeny {
		t.Errorf("bool false should map to ApprovalDeny, got %v", got)
	}
}
