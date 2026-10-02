//go:build !js

package webui

import (
	"encoding/json"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// decodePatch mirrors the handler: the client's JSON body, decoded as-is.
func decodePatch(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var patch map[string]interface{}
	if err := json.Unmarshal([]byte(body), &patch); err != nil {
		t.Fatalf("bad test body %s: %v", body, err)
	}
	return patch
}

// The webui writes one field at a time via updateSetting('section.field', v),
// which puts a literal dotted key on the wire. Every one of these was silently
// discarded: no applier claimed the key, so the field was reported in a
// "warnings" array the client ignores while the response stayed 200 and the UI
// showed "Saved".
func TestDottedSettingKeysAreApplied(t *testing.T) {
	tests := []struct {
		body   string
		verify func(*testing.T, *configuration.Config)
	}{
		{`{"computer_use.max_actions_per_minute":30}`, func(t *testing.T, c *configuration.Config) {
			if c.ComputerUse == nil || c.ComputerUse.MaxActionsPerMinute != 30 {
				t.Errorf("computer_use.max_actions_per_minute did not persist: %+v", c.ComputerUse)
			}
		}},
		{`{"computer_use.workspace_allowlist":["/a","/b"]}`, func(t *testing.T, c *configuration.Config) {
			if c.ComputerUse == nil || len(c.ComputerUse.WorkspaceAllowlist) != 2 {
				t.Errorf("computer_use.workspace_allowlist did not persist: %+v", c.ComputerUse)
			}
		}},
		{`{"mcp.enabled":true}`, func(t *testing.T, c *configuration.Config) {
			if !c.MCP.Enabled {
				t.Errorf("mcp.enabled did not persist: %+v", c.MCP)
			}
		}},
		{`{"computer_use.enabled":true}`, func(t *testing.T, c *configuration.Config) {
			if c.ComputerUse == nil || !c.ComputerUse.Enabled {
				t.Errorf("computer_use.enabled did not persist: %+v", c.ComputerUse)
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.body, func(t *testing.T) {
			cfg := &configuration.Config{}
			unknown, err := applyPartialSettings(cfg, decodePatch(t, tc.body))
			if err != nil {
				t.Fatalf("applyPartialSettings: %v", err)
			}
			if len(unknown) != 0 {
				t.Errorf("key was reported unknown (silently dropped): %v", unknown)
			}
			tc.verify(t, cfg)
		})
	}
}

// Section appliers rebuild their struct wholesale from the patch, so expanding
// a dotted key without seeding from the current config would turn a silent
// no-op into a silent wipe of every sibling field.
func TestDottedWriteDoesNotClobberSiblings(t *testing.T) {
	cfg := &configuration.Config{
		ComputerUse: &configuration.ComputerUseConfig{
			Enabled:             true,
			MaxActionsPerMinute: 5,
			WorkspaceAllowlist:  []string{"/a", "/b"},
			AuditLogDir:         "/tmp/audit",
		},
	}

	if _, err := applyPartialSettings(cfg, decodePatch(t, `{"computer_use.max_actions_per_minute":9}`)); err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}

	got := cfg.ComputerUse
	if got.MaxActionsPerMinute != 9 {
		t.Errorf("MaxActionsPerMinute = %d, want 9", got.MaxActionsPerMinute)
	}
	if !got.Enabled {
		t.Error("Enabled clobbered")
	}
	if got.AuditLogDir != "/tmp/audit" {
		t.Errorf("AuditLogDir = %q, want /tmp/audit", got.AuditLogDir)
	}
	if len(got.WorkspaceAllowlist) != 2 {
		t.Errorf("WorkspaceAllowlist = %v, want 2 entries", got.WorkspaceAllowlist)
	}
}

// Flat keys must keep flowing through untouched.
func TestFlatKeysStillApply(t *testing.T) {
	cfg := &configuration.Config{}
	unknown, err := applyPartialSettings(cfg, decodePatch(t, `{"reasoning_effort":"high"}`))
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want none", unknown)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want high", cfg.ReasoningEffort)
	}
}

// A dotted key under a section with no applier stays unknown rather than being
// silently accepted.
func TestDottedKeyWithoutApplierIsReportedUnknown(t *testing.T) {
	cfg := &configuration.Config{}
	unknown, err := applyPartialSettings(cfg, decodePatch(t, `{"security_validation.threshold":1}`))
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	if len(unknown) == 0 {
		t.Error("a section with no applier should be reported unknown")
	}
}

func ptrTo[T any](v T) *T { return &v }
