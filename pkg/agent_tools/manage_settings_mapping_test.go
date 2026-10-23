package tools

import (
	"strings"
	"testing"
)

// The tool validates against the agent's authoritative key registry, but its
// field switches only map a subset. A key that is real but unmapped must fail
// loudly: reporting "updated" without touching the config is worse than
// refusing, and reporting it as an unknown key is wrong.
func TestUnmappedSettingKeyIsRefusedNotSilentlyAccepted(t *testing.T) {
	SetSettingKeys(func() []string {
		return []string{"provider", "model", "risk_profile"}
	})
	t.Cleanup(func() { SetSettingKeys(nil) })

	if err := ValidateSettingKey("risk_profile"); err != nil {
		t.Fatalf("risk_profile should be a known key: %v", err)
	}

	err := setConfigField(nil, "risk_profile", "elevated")
	if err == nil {
		t.Fatal("setConfigField accepted an unmapped key; it must refuse")
	}
	if !strings.Contains(err.Error(), "not supported by manage_settings field mapping") {
		t.Errorf("error should name the missing mapping, got: %v", err)
	}

	if _, err := getConfigField(nil, "risk_profile"); err == nil {
		t.Fatal("getConfigField accepted an unmapped key; it must refuse")
	}

	// A genuinely unknown key still reports as unknown, not as unmapped.
	if err := setConfigField(nil, "not_a_setting", "x"); err == nil {
		t.Fatal("unknown key was accepted")
	} else if strings.Contains(err.Error(), "field mapping") {
		t.Errorf("unknown key should not be reported as unmapped: %v", err)
	}
}
