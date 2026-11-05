package configuration

import (
	"testing"
)

// TestApplyAutoRiskJudgeDefault_SetsMissingKey verifies the SP-168
// auto LLM risk-judge default: a fresh config (no auto_risk_judge key)
// gets auto_risk_judge=true after applyV2Defaults runs. The LLM risk
// analysis that already runs for Medium/High shell commands doubles as
// the approver unless the user opts out.
func TestApplyAutoRiskJudgeDefault_SetsMissingKey(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	cfg := mgr.GetConfig()
	if cfg == nil {
		t.Fatal("GetConfig returned nil")
	}
	if !cfg.AutoRiskJudge {
		t.Fatal("default AutoRiskJudge = false, want true (SP-168 auto risk-judge default)")
	}
}

// TestApplyAutoRiskJudgeDefault_PreservesUserFalse verifies the escape
// hatch: a user who wants an explicit human prompt for every Medium/High
// command sets auto_risk_judge=false and keeps that value across loads.
func TestApplyAutoRiskJudgeDefault_PreservesUserFalse(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfig(func(c *Config) error {
		c.AutoRiskJudge = false
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if err := mgr.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	cfg := mgr.GetConfig()
	if cfg.AutoRiskJudge {
		t.Fatal("explicit false override did not stick after SaveConfig")
	}
}
