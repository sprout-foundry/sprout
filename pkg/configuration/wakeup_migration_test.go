package configuration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigrateV2ToV2_1_WakeupArtifactRepair pins the auto-resume default
// regression: the pre-pointer partial-settings patch persisted the exact
// {"enabled": false, budgets 0,0} block into user configs, permanently
// disabling auto-resume (which defaults to ON). The migration drops the
// artifact so defaults apply.
func TestMigrateV2ToV2_1_WakeupArtifactRepair(t *testing.T) {
	artifact := map[string]interface{}{
		"version": "2.0",
		"wakeup": map[string]interface{}{
			"enabled":                 false,
			"max_tokens_per_session":  float64(0),
			"max_resumes_per_session": float64(0),
		},
	}
	migrated, err := MigrateConfig(artifact, "2.1")
	require.NoError(t, err)
	require.NotContains(t, migrated, "wakeup", "all-zeros artifact must be dropped so the enabled-by-default seed applies")
	require.Equal(t, "2.1", migrated["version"])

	// Round-trip through Load-style unmarshal: NewConfig seed + no wakeup key.
	cfg := NewConfig()
	data, err := json.Marshal(migrated)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, cfg))
	require.True(t, cfg.Wakeup.Enabled, "post-migration config must auto-resume enabled by default")
	require.Equal(t, 5000, cfg.Wakeup.MaxTokensPerSession)
	require.Equal(t, 10, cfg.Wakeup.MaxResumesPerSession)
}

func TestMigrateV2ToV2_1_PreservesDeliberateOptOut(t *testing.T) {
	// Deliberate opt-out: budgets at non-zero (materialized by a normal save).
	deliberate := map[string]interface{}{
		"version": "2.0",
		"wakeup": map[string]interface{}{
			"enabled":                 false,
			"max_tokens_per_session":  float64(5000),
			"max_resumes_per_session": float64(10),
		},
	}
	migrated, err := MigrateConfig(deliberate, "2.1")
	require.NoError(t, err)
	w, ok := migrated["wakeup"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, w["enabled"], "explicit false with materialized budgets is a deliberate opt-out")

	// Hand-written minimal opt-out: {"enabled": false} only — budgets absent.
	minimal := map[string]interface{}{
		"version": "2.0",
		"wakeup": map[string]interface{}{
			"enabled": false,
		},
	}
	migrated, err = MigrateConfig(minimal, "2.1")
	require.NoError(t, err)
	w, ok = migrated["wakeup"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, w["enabled"], "hand-written minimal false is kept")
}

func TestMigrateV2ToV2_1_NoWakeupBlockIsNoop(t *testing.T) {
	raw := map[string]interface{}{"version": "2.0", "provider": "x"}
	migrated, err := MigrateConfig(raw, "2.1")
	require.NoError(t, err)
	require.Equal(t, "2.1", migrated["version"])
	require.NotContains(t, migrated, "wakeup")
}

func TestMigrateV2ToV2_1_EnabledConfigUntouched(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"wakeup": map[string]interface{}{
			"enabled":                 true,
			"max_tokens_per_session":  float64(0),
			"max_resumes_per_session": float64(0),
		},
	}
	migrated, err := MigrateConfig(raw, "2.1")
	require.NoError(t, err)
	w, ok := migrated["wakeup"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, w["enabled"])
}

// TestMigrateChain_2_1_To_3_0 verifies SP-133's persona-tools step is still
// reachable after re-anchoring it from 2.0 to 2.1.
func TestMigrateChain_2_1_To_3_0(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"orchestrator": map[string]interface{}{
				"id":            "orchestrator",
				"name":          "Orchestrator",
				"description":   "d",
				"enabled":       true,
				"allowed_tools": []interface{}{"shell_command"},
			},
		},
	}
	migrated, err := MigrateConfig(raw, "3.0")
	require.NoError(t, err)
	require.Equal(t, "3.0", migrated["version"])
	st, ok := migrated["subagent_types"].(map[string]interface{})
	require.True(t, ok)
	orch, ok := st["orchestrator"].(map[string]interface{})
	require.True(t, ok)
	tools, ok := orch["allowed_tools"].([]interface{})
	require.True(t, ok)
	toolSet := make(map[string]bool)
	for _, tl := range tools {
		toolSet[tl.(string)] = true
	}
	require.True(t, toolSet["shell_command"], "existing tool preserved through chained 2.0→2.1→3.0 migration")
}
