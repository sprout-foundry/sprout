package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// manage_settings lives in pkg/agent_tools and cannot import this package
// (cycle), so it validated against a hand-copied key list that had fallen
// behind the settingDefs registry. This pins the wiring that hands it the
// authoritative list.
func TestManageSettingsValidatesAgainstAgentRegistry(t *testing.T) {
	keys := SupportedSettingKeys()
	if len(keys) == 0 {
		t.Fatal("SupportedSettingKeys returned no keys")
	}
	for _, k := range keys {
		if err := tools.ValidateSettingKey(k); err != nil {
			t.Errorf("manage_settings rejects %q, which the agent registry defines: %v", k, err)
		}
	}
	if err := tools.ValidateSettingKey("definitely_not_a_setting"); err == nil {
		t.Error("an unknown key was accepted")
	}
}
