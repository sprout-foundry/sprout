package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSP168_BuildToolEnvPropagatesGate1Decided pins the env-wiring half of
// the single-authoritative-gate change: buildToolEnvFromAgent copies the
// unified gate's per-call decision into the ToolEnv (so handler-level gates
// can skip their prompt), and ONLY when the unified resolver is the active
// gate — under the legacy dual-gate resolver, Gate 2 keeps its own
// prompts and the unified flag must not leak through.
func TestSP168_BuildToolEnvPropagatesGate1Decided(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	agent.SetWorkspaceRoot(t.TempDir())

	setUnified := func(enabled bool) {
		if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.UnifiedRiskResolver = enabled
			return nil
		}); err != nil {
			t.Fatalf("config update failed: %v", err)
		}
	}

	// Legacy resolver (the fresh-test default): the unified flag must not
	// be propagated even if a previous unified gate decided a call.
	setUnified(false)
	agent.markGate1Decided()
	if env := buildToolEnvFromAgent(agent); env.Gate1Decided {
		t.Error("Gate1Decided must not propagate under the legacy resolver")
	}

	// Unified resolver: a decided call propagates to the ToolEnv.
	setUnified(true)
	agent.markGate1Decided()
	if env := buildToolEnvFromAgent(agent); !env.Gate1Decided {
		t.Error("Gate1Decided should propagate to the ToolEnv after a unified-gate decision")
	}

	// A fresh (non-decided) call propagates false.
	agent.resetGate1Decided()
	if env := buildToolEnvFromAgent(agent); env.Gate1Decided {
		t.Error("a call the unified gate has not decided must not carry the flag")
	}
}
