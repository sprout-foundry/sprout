package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// The compaction ceiling is settable only through the settings key, so the
// key's validation is the only guard on the value.
func TestSettingCompactionCeiling(t *testing.T) {
	cfg := configuration.NewConfig()

	// Registered and visible.
	found := false
	for _, k := range SupportedSettingKeys() {
		if k == "compaction_ceiling_tokens" {
			found = true
		}
	}
	if !found {
		t.Fatal("compaction_ceiling_tokens not in SupportedSettingKeys")
	}

	if err := SetSettingValue(cfg, "compaction_ceiling_tokens", "500000"); err != nil {
		t.Fatalf("set 500000: %v", err)
	}
	if cfg.CompactionCeilingTokens != 500000 {
		t.Fatalf("got %d, want 500000", cfg.CompactionCeilingTokens)
	}

	// 0 resets to the built-in default.
	if err := SetSettingValue(cfg, "compaction_ceiling_tokens", "0"); err != nil {
		t.Fatalf("set 0: %v", err)
	}
	if cfg.CompactionCeilingTokens != 0 {
		t.Fatalf("got %d, want 0", cfg.CompactionCeilingTokens)
	}

	// Below the floor is rejected.
	if err := SetSettingValue(cfg, "compaction_ceiling_tokens", "1024"); err == nil {
		t.Fatal("expected rejection for 1024")
	}
	// Non-integer is rejected.
	if err := SetSettingValue(cfg, "compaction_ceiling_tokens", "big"); err == nil {
		t.Fatal("expected rejection for non-integer")
	}
}
