package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// TestDesignerPersona_CatalogContract covers SP-140-2 §2a at the layer the
// agent actually reads: the designer persona must resolve by canonical ID and
// by both aliases out of the real embedded catalog, must be delegatable, and
// must carry the git_write capability (checked via HasCapability, the only
// legitimate capability check).
func TestDesignerPersona_CatalogContract(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	resolve := func(id string) *string {
		st := agent.configManager.GetConfig().GetSubagentType(id)
		if st == nil {
			return nil
		}
		canonical := st.ID
		return &canonical
	}

	canonical := resolve(personas.IDDesigner)
	if canonical == nil {
		t.Fatalf("designer persona must be present in the catalog")
	}
	if *canonical != personas.IDDesigner {
		t.Errorf("designer ID = %q, want %q", *canonical, personas.IDDesigner)
	}

	for _, alias := range []string{"ux", "design"} {
		got := resolve(alias)
		if got == nil {
			t.Errorf("alias %q must resolve to a persona", alias)
			continue
		}
		if *got != personas.IDDesigner {
			t.Errorf("alias %q resolved to %q, want designer", alias, *got)
		}
	}

	designer := agent.configManager.GetConfig().GetSubagentType(personas.IDDesigner)
	if !designer.Delegatable {
		t.Error("designer must be delegatable")
	}
	if !designer.HasCapability(personas.CapabilityGitWrite) {
		t.Error("designer must carry the git_write capability")
	}
	for _, tool := range []string{"design_validate", "design_assets", "design_render", "design_import_sketch"} {
		if !containsString(designer.AllowedTools, tool) {
			t.Errorf("designer allowed_tools must include %q", tool)
		}
	}
}

// TestOrchestratorCanSpawnDesigner is the delegatable-path assertion from the
// SP-140-2 acceptance criteria: an orchestrator (a Delegatable=true-free
// persona that lacks can_spawn_non_delegatable entries) must be able to spawn
// designer through the normal delegatable gate in handleRunSubagent — i.e.
// designer's Delegatable=true short-circuits the can_spawn_non_delegatable
// requirement.
func TestOrchestratorCanSpawnDesigner(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.ApplyPersona(personas.IDOrchestrator); err != nil {
		t.Fatalf("failed to activate orchestrator: %v", err)
	}
	setupTestSubagentRunner(agent)

	result, err := handleRunSubagent(context.Background(), agent, map[string]interface{}{
		"prompt":  "design a mobile check-deposit flow",
		"persona": personas.IDDesigner,
	})
	if err != nil {
		t.Fatalf("orchestrator must be able to spawn designer, got error: %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Error("expected non-empty subagent result")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}
