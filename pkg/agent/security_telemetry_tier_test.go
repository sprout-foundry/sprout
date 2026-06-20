package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Task 3: Telemetry counters
// ---------------------------------------------------------------------------

func TestSecurityTelemetry_CountersStartAtZero(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	if got := a.GetSecurityCautionsIssued(); got != 0 {
		t.Errorf("CautionsIssued = %d, want 0", got)
	}
	if got := a.GetSecurityRetriesAfterCaution(); got != 0 {
		t.Errorf("RetriesAfterCaution = %d, want 0", got)
	}
	if got := a.GetSecurityLoopsDetected(); got != 0 {
		t.Errorf("LoopsDetected = %d, want 0", got)
	}
}

func TestSecurityTelemetry_NilAgentSafe(t *testing.T) {
	t.Parallel()

	var a *Agent
	if got := a.GetSecurityCautionsIssued(); got != 0 {
		t.Errorf("nil CautionsIssued = %d, want 0", got)
	}
	if got := a.GetSecurityRetriesAfterCaution(); got != 0 {
		t.Errorf("nil RetriesAfterCaution = %d, want 0", got)
	}
	if got := a.GetSecurityLoopsDetected(); got != 0 {
		t.Errorf("nil LoopsDetected = %d, want 0", got)
	}
}

func TestSecurityTelemetry_IncrementMethods(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	a.incrementSecurityCautionsIssued()
	a.incrementSecurityCautionsIssued()
	if got := a.GetSecurityCautionsIssued(); got != 2 {
		t.Errorf("CautionsIssued = %d, want 2", got)
	}

	a.incrementSecurityRetryAfterCaution()
	if got := a.GetSecurityRetriesAfterCaution(); got != 1 {
		t.Errorf("RetriesAfterCaution = %d, want 1", got)
	}

	a.incrementSecurityLoopsDetected()
	a.incrementSecurityLoopsDetected()
	if got := a.GetSecurityLoopsDetected(); got != 2 {
		t.Errorf("LoopsDetected = %d, want 2", got)
	}
}

func TestSecurityTelemetry_NilAgentIncrementSafe(t *testing.T) {
	t.Parallel()

	var a *Agent
	a.incrementSecurityCautionsIssued()
	a.incrementSecurityRetryAfterCaution()
	a.incrementSecurityLoopsDetected()
	// Must not panic
}

func TestSecurityTelemetry_wrapSecurityCautionIncrements(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	err := agenterrors.NewSecurityError("security confirmation required: test", nil)

	// wrapSecurityCaution (the non-loop variant) should increment cautions.
	wrapSecurityCaution(a, err)
	if got := a.GetSecurityCautionsIssued(); got != 1 {
		t.Errorf("after wrapSecurityCaution: CautionsIssued = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Task 4: Tier-aware caution suffixes
// ---------------------------------------------------------------------------

func TestTierFromMessage_HardBlock(t *testing.T) {
	t.Parallel()

	msg := "security hard block: rm -rf / — cannot be approved"
	suffix := tierFromMessage(msg)
	if !contains(suffix, "unconditionally blocked") {
		t.Errorf("hard-block suffix should mention 'unconditionally blocked': %q", suffix)
	}
	if !contains(suffix, "Do not attempt it again") {
		t.Errorf("hard-block suffix should contain 'Do not attempt it again': %q", suffix)
	}
}

func TestTierFromMessage_ConfirmationRequired(t *testing.T) {
	t.Parallel()

	msg := "security confirmation required: write_file — needs approval"
	suffix := tierFromMessage(msg)
	if !contains(suffix, "interactive user approval") {
		t.Errorf("confirmation suffix should mention 'interactive user approval': %q", suffix)
	}
	if !contains(suffix, "ask_user") {
		t.Errorf("confirmation suffix should mention 'ask_user': %q", suffix)
	}
}

func TestTierFromMessage_Rejected(t *testing.T) {
	t.Parallel()

	msg := "security rejected: user declined approval"
	suffix := tierFromMessage(msg)
	if !contains(suffix, "user declined") {
		t.Errorf("rejected suffix should mention 'user declined': %q", suffix)
	}
	if !contains(suffix, "fundamentally different approach") {
		t.Errorf("rejected suffix should mention 'fundamentally different approach': %q", suffix)
	}
}

func TestTierFromMessage_Default(t *testing.T) {
	t.Parallel()

	msg := "some generic security error"
	suffix := tierFromMessage(msg)
	if !contains(suffix, "Do not retry this exact operation") {
		t.Errorf("default suffix should contain generic guidance: %q", suffix)
	}
}

func TestTierPrefixFromMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		msg  string
		want string
	}{
		{"security hard block: critical op", "hard-block"},
		{"security confirmation required: needs approval", "confirmation"},
		{"security rejected: declined", "rejected"},
		{"unknown security error", "caution"},
	}
	for _, tc := range cases {
		if got := tierPrefixFromMessage(tc.msg); got != tc.want {
			t.Errorf("tierPrefixFromMessage(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier-aware suffix in wrapSecurityCaution (integration)
// ---------------------------------------------------------------------------

func TestWrapSecurityCaution_TierAwareHardBlock(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	err := agenterrors.NewSecurityError("security hard block: rm -rf / — cannot be approved", nil)
	wrapped := wrapSecurityCaution(a, err)

	if !contains(wrapped.Error(), "SECURITY_CAUTION_REQUIRED") {
		t.Errorf("should contain SECURITY_CAUTION_REQUIRED: %s", wrapped.Error())
	}
	if !contains(wrapped.Error(), "unconditionally blocked") {
		t.Errorf("hard-block should have tier-aware suffix: %s", wrapped.Error())
	}
}

func TestWrapSecurityCaution_TierAwareConfirmation(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	err := agenterrors.NewSecurityError("security confirmation required: write outside workspace", nil)
	wrapped := wrapSecurityCaution(a, err)

	if !contains(wrapped.Error(), "interactive user approval") {
		t.Errorf("confirmation should have tier-aware suffix: %s", wrapped.Error())
	}
}

func TestWrapSecurityCaution_TierAwareRejected(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	err := agenterrors.NewSecurityError("user rejected shell_command", nil)
	wrapped := wrapSecurityCaution(a, err)

	if !contains(wrapped.Error(), "user declined") {
		t.Errorf("rejected should have tier-aware suffix: %s", wrapped.Error())
	}
}

// ---------------------------------------------------------------------------
// handleToolError tier-aware suffix (integration)
// ---------------------------------------------------------------------------

func TestHandleToolError_TierAwareSuffix(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}

	// Hard block tier
	secErr := agenterrors.NewSecurityError("security hard block: rm -rf / — cannot be approved", nil)
	_, wrapped := handleToolError(a, secErr, "shell_command")
	if !contains(wrapped.Error(), "unconditionally blocked") {
		t.Errorf("handleToolError hard-block should have tier suffix: %s", wrapped.Error())
	}
}

func TestHandleToolError_TelemetryIncrement(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	secErr := agenterrors.NewSecurityError("security confirmation required: test", nil)

	before := a.GetSecurityCautionsIssued()
	handleToolError(a, secErr, "shell_command")
	after := a.GetSecurityCautionsIssued()

	if after != before+1 {
		t.Errorf("handleToolError should increment cautions: before=%d after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// riskCategoryFromAssessment
// ---------------------------------------------------------------------------

func TestRiskCategoryFromAssessment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		assessment  RiskAssessment
		wantLevel   string
	}{
		{"critical", RiskAssessment{Level: configuration.RiskLevelCritical}, "critical"},
		{"high", RiskAssessment{Level: configuration.RiskLevelHigh}, "high"},
		{"medium", RiskAssessment{Level: configuration.RiskLevelMedium}, "medium"},
		{"low", RiskAssessment{Level: configuration.RiskLevelLow}, "low"},
	}
	for _, tc := range cases {
		if got := riskCategoryFromAssessment(tc.assessment); got != tc.wantLevel {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.wantLevel)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON exposure in metrics struct (cmd package) — verify field names
// ---------------------------------------------------------------------------

func TestSecurityTelemetry_JSONFieldNames(t *testing.T) {
	// Verify the field names match what emitJSONResult populates. We can't
	// easily import cmd.AgentResultMetrics here, so we just verify the
	// getter names are consistent and JSON-serializable.
	a := &Agent{}
	a.incrementSecurityCautionsIssued()
	a.incrementSecurityRetryAfterCaution()
	a.incrementSecurityLoopsDetected()

	// Simulate what cmd/agent_result.go does: build a map from getters.
	metrics := map[string]int64{
		"security_cautions_issued":        a.GetSecurityCautionsIssued(),
		"security_retries_after_caution":  a.GetSecurityRetriesAfterCaution(),
		"security_loops_detected":         a.GetSecurityLoopsDetected(),
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	jsonStr := string(data)

	requiredFields := []string{
		`"security_cautions_issued":1`,
		`"security_retries_after_caution":1`,
		`"security_loops_detected":1`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("JSON missing expected field %q in: %s", field, jsonStr)
		}
	}
}
