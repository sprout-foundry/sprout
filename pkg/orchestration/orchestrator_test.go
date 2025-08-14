package orchestration

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestSortStepsByDependencies_KahnOrder(t *testing.T) {
	o := &MultiAgentOrchestrator{}
	steps := []types.OrchestrationStep{
		{ID: "s1", Name: "S1"},
		{ID: "s2", Name: "S2", DependsOn: []string{"s1"}},
		{ID: "s3", Name: "S3", DependsOn: []string{"s2"}},
	}
	ordered := o.sortStepsByDependencies(steps)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(ordered))
	}
	if ordered[0].ID != "s1" || ordered[1].ID != "s2" || ordered[2].ID != "s3" {
		t.Fatalf("unexpected order: %v", []string{ordered[0].ID, ordered[1].ID, ordered[2].ID})
	}
}

func TestExecuteSteps_DeadlockDetection(t *testing.T) {
	// Create a plan where no steps are runnable (circular dependency)
	plan := &types.MultiAgentOrchestrationPlan{
		Goal:   "Deadlock test",
		Agents: []types.AgentDefinition{{ID: "a1", Name: "A"}},
		Steps: []types.OrchestrationStep{
			{ID: "s1", Name: "S1", AgentID: "a1", DependsOn: []string{"s2"}},
			{ID: "s2", Name: "S2", AgentID: "a1", DependsOn: []string{"s1"}},
		},
		AgentStatuses: map[string]types.AgentStatus{"a1": {Status: "idle"}},
	}
	o := &MultiAgentOrchestrator{plan: plan, concurrency: 1, logger: utils.GetLogger(true)}
	err := o.executeSteps()
	if err == nil {
		t.Fatalf("expected deadlock error, got nil")
	}
	if !strings.Contains(err.Error(), "no runnable steps") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureCompatibility(t *testing.T) {
	base := &types.MultiAgentOrchestrationPlan{
		Goal:          "Goal A",
		Agents:        []types.AgentDefinition{{ID: "a1"}},
		Steps:         []types.OrchestrationStep{{ID: "s1"}},
		AgentStatuses: map[string]types.AgentStatus{"a1": {}},
	}
	o := &MultiAgentOrchestrator{plan: base}

	// Matching plan
	saved := *base
	if err := o.ensureCompatibility(&saved); err != nil {
		t.Fatalf("unexpected incompatibility: %v", err)
	}

	// Different goal
	saved2 := *base
	saved2.Goal = "Other"
	if err := o.ensureCompatibility(&saved2); err == nil {
		t.Fatalf("expected incompatibility due to goal mismatch")
	}

	// Missing agent
	saved3 := *base
	saved3.Agents = []types.AgentDefinition{}
	if err := o.ensureCompatibility(&saved3); err == nil {
		t.Fatalf("expected incompatibility due to agent set mismatch")
	}

	// Missing step
	saved4 := *base
	saved4.Steps = []types.OrchestrationStep{}
	if err := o.ensureCompatibility(&saved4); err == nil {
		t.Fatalf("expected incompatibility due to step set mismatch")
	}
}

func TestListRunnableStepIDs(t *testing.T) {
	plan := &types.MultiAgentOrchestrationPlan{
		Agents: []types.AgentDefinition{{ID: "a1"}},
		Steps: []types.OrchestrationStep{
			{ID: "s1", AgentID: "a1", Status: "completed"},
			{ID: "s2", AgentID: "a1", Status: "pending", DependsOn: []string{"s1"}},
			{ID: "s3", AgentID: "a1", Status: "pending", DependsOn: []string{"s2"}},
		},
		AgentStatuses: map[string]types.AgentStatus{"a1": {}},
	}
	o := &MultiAgentOrchestrator{plan: plan, logger: utils.GetLogger(true)}
	ids := o.listRunnableStepIDs()
	if len(ids) != 1 || ids[0] != "s2" {
		t.Fatalf("expected only s2 runnable, got %v", ids)
	}
}
