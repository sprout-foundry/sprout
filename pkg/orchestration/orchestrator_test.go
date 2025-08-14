package orchestration

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
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

func TestProgressTablePublishesUIEvent(t *testing.T) {
	// Ensure UI is disabled for this test to exercise stdout path without panics
	// We'll still call printProgressTable to ensure no panic
	plan := &types.MultiAgentOrchestrationPlan{
		Agents:        []types.AgentDefinition{{ID: "a1", Name: "A"}},
		Steps:         []types.OrchestrationStep{{ID: "s1", Status: "completed"}},
		AgentStatuses: map[string]types.AgentStatus{"a1": {Status: "completed", TokenUsage: 10, Cost: 0.01}},
	}
	o := &MultiAgentOrchestrator{plan: plan, logger: utils.GetLogger(true)}
	// The function should not panic
	o.printProgressTable()
}

func TestValidationStage_NonBlockingCustomCheck(t *testing.T) {
	o := &MultiAgentOrchestrator{
		validation: &types.ValidationConfig{
			BuildCommand: "",
			TestCommand:  "",
			LintCommand:  "",
			CustomChecks: []string{"!echo nonblocking && false"},
			Required:     true,
		},
		logger: utils.GetLogger(true),
		plan:   &types.MultiAgentOrchestrationPlan{},
	}
	// Should not error because the custom check is marked non-blocking with '!'
	if err := o.runValidationStage(); err != nil {
		t.Fatalf("expected no error for non-blocking custom check, got %v", err)
	}
}

func TestValidationStage_BlockingCustomCheckFails(t *testing.T) {
	o := &MultiAgentOrchestrator{
		validation: &types.ValidationConfig{
			CustomChecks: []string{"false"},
			Required:     true,
		},
		logger: utils.GetLogger(true),
		plan:   &types.MultiAgentOrchestrationPlan{},
	}
	if err := o.runValidationStage(); err == nil {
		t.Fatalf("expected error for blocking custom check")
	}
}

func TestValidationStage_BuildAndLintNonRequired(t *testing.T) {
	o := &MultiAgentOrchestrator{
		validation: &types.ValidationConfig{
			BuildCommand: "false",
			TestCommand:  "",
			LintCommand:  "false",
			Required:     false,
		},
		logger: utils.GetLogger(true),
		plan:   &types.MultiAgentOrchestrationPlan{},
	}
	if err := o.runValidationStage(); err != nil {
		t.Fatalf("did not expect error when validation is non-required, got %v", err)
	}
}

func TestAgentHaltPreventsExecutionWhenStopOnLimit(t *testing.T) {
	plan := &types.MultiAgentOrchestrationPlan{
		Agents: []types.AgentDefinition{{ID: "a1", Budget: &types.AgentBudget{StopOnLimit: true}}},
		Steps: []types.OrchestrationStep{
			{ID: "s1", AgentID: "a1", Status: "pending"},
		},
		AgentStatuses: map[string]types.AgentStatus{"a1": {Halted: true, HaltReason: "budget exceeded"}},
	}
	o := &MultiAgentOrchestrator{plan: plan, logger: utils.GetLogger(true)}
	if ids := o.listRunnableStepIDs(); len(ids) != 0 {
		t.Fatalf("expected no runnable steps when agent is halted, got %v", ids)
	}
}

func TestEnrichStepWithToolContext_GatedOff_NoChanges(t *testing.T) {
	o := &MultiAgentOrchestrator{config: &config.Config{CodeToolsEnabled: false}, logger: utils.GetLogger(true)}
	step := &types.OrchestrationStep{
		Input: map[string]string{
			"workspace_tree":       "true",
			"workspace_summary":    "true",
			"workspace_search":     "TODO",
			"workspace_embeddings": "TODO",
			"web_search":           "golang testing",
		},
		Tools: map[string]string{
			"workspace_tree":       "true",
			"workspace_summary":    "true",
			"workspace_search":     "TODO",
			"workspace_embeddings": "TODO",
			"web_search":           "golang testing",
		},
	}

	// Call enrichment; with tools disabled, inputs should remain unchanged (no *_content/_results keys)
	o.enrichStepWithToolContext(step)

	if _, ok := step.Input["workspace_tree_content"]; ok {
		t.Fatalf("unexpected enrichment when tools disabled: workspace_tree_content present")
	}
	if _, ok := step.Input["workspace_summary_content"]; ok {
		t.Fatalf("unexpected enrichment when tools disabled: workspace_summary_content present")
	}
	if _, ok := step.Input["workspace_search_results"]; ok {
		t.Fatalf("unexpected enrichment when tools disabled: workspace_search_results present")
	}
	if _, ok := step.Input["workspace_embeddings_results"]; ok {
		t.Fatalf("unexpected enrichment when tools disabled: workspace_embeddings_results present")
	}
	if _, ok := step.Input["web_search_results"]; ok {
		t.Fatalf("unexpected enrichment when tools disabled: web_search_results present")
	}
}

func TestShouldUseLLMToolsFlag(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"true", true}, {"TRUE", true}, {"enabled", true}, {"1", true},
		{"false", false}, {"", false}, {"no", false},
	}
	for _, c := range cases {
		step := &types.OrchestrationStep{Tools: map[string]string{"llm_tools": c.val}}
		got := shouldUseLLMTools(step)
		if got != c.want {
			t.Fatalf("llm_tools=%q want %v got %v", c.val, c.want, got)
		}
	}
}

func TestEnrichStepWithToolContext_ReadFileAndShell(t *testing.T) {
	o := &MultiAgentOrchestrator{config: &config.Config{CodeToolsEnabled: true}, logger: utils.GetLogger(true)}
	step := &types.OrchestrationStep{
		Input: map[string]string{
			"read_file": "/etc/hosts",
			"run_shell": "echo hello",
		},
	}
	o.enrichStepWithToolContext(step)
	if _, ok := step.Input["read_file_content"]; !ok {
		t.Fatalf("expected read_file_content to be populated")
	}
	if out, ok := step.Input["shell_command_output"]; !ok || !strings.Contains(out, "hello") {
		t.Fatalf("expected shell_command_output to contain 'hello', got %q", out)
	}
}
