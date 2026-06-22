package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

func TestGetAvailablePersonaIDsSorted(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ids := agent.GetAvailablePersonaIDs()
	if len(ids) < 2 {
		t.Fatalf("expected at least two personas, got %d", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("persona ids are not sorted: %v", ids)
		}
	}
}

func TestGetPersonaProviderModelUsesProviderKeys(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	provider, _, err := agent.GetPersonaProviderModel("general")
	if err != nil {
		t.Fatalf("GetPersonaProviderModel failed: %v", err)
	}
	if provider != string(agent.GetProviderType()) {
		t.Fatalf("expected provider key %q, got %q", string(agent.GetProviderType()), provider)
	}
}

func TestGetPersonaProviderModelProviderOverrideUsesConfiguredModel(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if err := agent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes["tmp_provider_override"] = configuration.SubagentType{
			ID:       "tmp_provider_override",
			Name:     "Temp Provider Override",
			Provider: "deepinfra",
			Enabled:  true,
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to seed persona config: %v", err)
	}

	cfg := agent.GetConfigManager().GetConfig()

	provider, model, err := agent.GetPersonaProviderModel("tmp_provider_override")
	if err != nil {
		t.Fatalf("GetPersonaProviderModel failed: %v", err)
	}
	if provider != "deepinfra" {
		t.Fatalf("expected deepinfra provider, got %q", provider)
	}
	wantModel := cfg.GetModelForProvider("deepinfra")
	if model != wantModel {
		t.Fatalf("expected model %q, got %q", wantModel, model)
	}
}

func TestApplyPersonaNotFoundIncludesAvailablePersonas(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	err = agent.ApplyPersona("definitely_not_real")
	if err == nil {
		t.Fatalf("expected error for unknown persona")
	}
	msg := err.Error()
	if !strings.Contains(msg, "available personas:") {
		t.Fatalf("expected available personas in error, got: %s", msg)
	}
	if !strings.Contains(msg, "orchestrator") {
		t.Fatalf("expected orchestrator in available persona list, got: %s", msg)
	}
}

// =============================================================================
// IsLocalMode tests
// =============================================================================

func TestIsLocalMode_DefaultReturnsTrue(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if !agent.IsLocalMode() {
		t.Errorf("IsLocalMode() = false, want true (default)")
	}
}

func TestIsLocalMode_CloudEnvReturnsFalse(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	t.Setenv("SPROUT_CLOUD", "1")

	if agent.IsLocalMode() {
		t.Errorf("IsLocalMode() = true, want false (SPROUT_CLOUD=1)")
	}
}

func TestIsLocalMode_CloudEnvNonOneReturnsTrue(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	t.Setenv("SPROUT_CLOUD", "0")

	if !agent.IsLocalMode() {
		t.Errorf("IsLocalMode() = false, want true (SPROUT_CLOUD=0)")
	}
}

func TestIsLocalMode_CloudEnvEmptyReturnsTrue(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	t.Setenv("SPROUT_CLOUD", "")

	if !agent.IsLocalMode() {
		t.Errorf("IsLocalMode() = false, want true (SPROUT_CLOUD=empty)")
	}
}

// =============================================================================
// GetAvailablePersonaIDs LocalOnly filtering
// =============================================================================

func TestGetAvailablePersonaIDs_LocalOnlyFilteredInCloudMode(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Register a LocalOnly persona
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes["test_localonly_persona_x"] = configuration.SubagentType{
			ID:        "test_localonly_persona_x",
			Name:      "Local Only Test",
			Enabled:   true,
			LocalOnly: true,
		}
		// Also add a regular (non-local-only) persona
		cfg.SubagentTypes["test_regular_persona_x"] = configuration.SubagentType{
			ID:      "test_regular_persona_x",
			Name:    "Regular Test",
			Enabled: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// In local mode (default), both should be present
	ids := agent.GetAvailablePersonaIDs()
	localOnlyFound := false
	regularFound := false
	for _, id := range ids {
		if id == "test_localonly_persona_x" {
			localOnlyFound = true
		}
		if id == "test_regular_persona_x" {
			regularFound = true
		}
	}
	if !localOnlyFound {
		t.Error("LocalOnly persona should be present in local mode")
	}
	if !regularFound {
		t.Error("regular persona should be present in local mode")
	}

	// In cloud mode, LocalOnly persona should be filtered out
	t.Setenv("SPROUT_CLOUD", "1")
	ids = agent.GetAvailablePersonaIDs()
	for _, id := range ids {
		if id == "test_localonly_persona_x" {
			t.Error("LocalOnly persona should NOT be present in cloud mode")
		}
	}
	// Regular persona should still be present
	regularFound = false
	for _, id := range ids {
		if id == "test_regular_persona_x" {
			regularFound = true
		}
	}
	if !regularFound {
		t.Error("regular persona should still be present in cloud mode")
	}
}

func TestGetAvailablePersonaIDs_LocalOnlyDisabledStillExcludedInCloud(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes["test_disabled_localonly_x"] = configuration.SubagentType{
			ID:        "test_disabled_localonly_x",
			Name:      "Disabled Local Only",
			Enabled:   false,
			LocalOnly: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// Disabled + LocalOnly: should be excluded in both modes
	ids := agent.GetAvailablePersonaIDs()
	for _, id := range ids {
		if id == "test_disabled_localonly_x" {
			t.Error("disabled persona should never appear in available IDs (local mode)")
		}
	}

	t.Setenv("SPROUT_CLOUD", "1")
	ids = agent.GetAvailablePersonaIDs()
	for _, id := range ids {
		if id == "test_disabled_localonly_x" {
			t.Error("disabled persona should never appear in available IDs (cloud mode)")
		}
	}
}

// =============================================================================
// Agent.EvaluateOperationRisk tests
// =============================================================================

func TestEvaluateOperationRisk_NoPersonaReturnsLow(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Clear any auto-activated persona so we exercise the truly
	// no-persona path (Agent.EvaluateOperationRisk short-circuits to
	// Low when GetActivePersona returns "").
	agent.state.SetActivePersona("")

	// With no active persona at all, EvaluateOperationRisk skips
	// the cascade and returns Low (no gating).
	risk := agent.EvaluateOperationRisk("rm -rf /tmp")
	if risk != configuration.RiskLevelLow {
		t.Errorf("EvaluateOperationRisk with no persona = %q, want %q", risk, configuration.RiskLevelLow)
	}
}

func TestEvaluateOperationRisk_PersonaWithoutAutoApproveRulesAppliesProfile(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Activate a persona that has no AutoApproveRules (e.g., "coder").
	agent.state.SetActivePersona("coder")

	// SP-058 contract: personas without their own rules now fall
	// through to the active risk profile (default = EA-style rules).
	// "rm -rf /tmp" hits rm_recursive in the default profile's
	// HighRiskNever list and returns High.
	risk := agent.EvaluateOperationRisk("rm -rf /tmp")
	if risk != configuration.RiskLevelHigh {
		t.Errorf("EvaluateOperationRisk with rules-less persona + default profile = %q, want %q", risk, configuration.RiskLevelHigh)
	}

	// And a benign command stays Medium under the default profile.
	if got := agent.EvaluateOperationRisk("echo hello"); got != configuration.RiskLevelMedium {
		t.Errorf("EvaluateOperationRisk(\"echo hello\") = %q, want %q", got, configuration.RiskLevelMedium)
	}
}

func TestEvaluateOperationRisk_CriticalAlwaysBlocks(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Even with no persona, Critical patterns return Critical.
	agent.state.SetActivePersona("")
	if got := agent.EvaluateOperationRisk("rm -rf /"); got != configuration.RiskLevelCritical {
		t.Errorf("rm -rf / = %q, want %q", got, configuration.RiskLevelCritical)
	}

	// Unrestricted profile cannot override Critical.
	agent.SetRiskProfileOverride(configuration.RiskProfileUnrestricted)
	agent.state.SetActivePersona("coder")
	if got := agent.EvaluateOperationRisk("rm -rf /"); got != configuration.RiskLevelCritical {
		t.Errorf("unrestricted+coder rm -rf / = %q, want %q", got, configuration.RiskLevelCritical)
	}
}

func TestEvaluateOperationRisk_ProfileOverrideTakesEffect(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	agent.state.SetActivePersona("coder") // rules-less; falls through to profile

	// Default profile classifies `git push` as Medium.
	if got := agent.EvaluateOperationRisk("git push origin main"); got != configuration.RiskLevelMedium {
		t.Errorf("default profile git push = %q, want Medium", got)
	}

	// Permissive profile classifies `git push` as Low.
	agent.SetRiskProfileOverride(configuration.RiskProfilePermissive)
	if got := agent.EvaluateOperationRisk("git push origin main"); got != configuration.RiskLevelLow {
		t.Errorf("permissive profile git push = %q, want Low", got)
	}

	// Cautious profile classifies `git push` as High (prompt path).
	agent.SetRiskProfileOverride(configuration.RiskProfileCautious)
	if got := agent.EvaluateOperationRisk("git push origin main"); got != configuration.RiskLevelHigh {
		t.Errorf("cautious profile git push = %q, want High", got)
	}
}

func TestEvaluateOperationRisk_PersonaWithAutoApproveRulesReturnsCorrectRisk(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Register a persona with auto-approve rules
	rules := configuration.DefaultAutoApproveRules()
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes["test_ea_persona_risk"] = configuration.SubagentType{
			ID:               "test_ea_persona_risk",
			Name:             "Test EA",
			Enabled:          true,
			AutoApproveRules: &rules,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	agent.state.SetActivePersona("test_ea_persona_risk")

	// Low risk: git status
	if risk := agent.EvaluateOperationRisk("git status"); risk != configuration.RiskLevelLow {
		t.Errorf("git status risk = %q, want %q", risk, configuration.RiskLevelLow)
	}

	// Medium risk: git commit
	if risk := agent.EvaluateOperationRisk("git commit -m msg"); risk != configuration.RiskLevelMedium {
		t.Errorf("git commit risk = %q, want %q", risk, configuration.RiskLevelMedium)
	}

	// High risk: rm -rf
	if risk := agent.EvaluateOperationRisk("rm -rf /tmp"); risk != configuration.RiskLevelHigh {
		t.Errorf("rm -rf risk = %q, want %q", risk, configuration.RiskLevelHigh)
	}

	// High risk: git reset --hard
	if risk := agent.EvaluateOperationRisk("git reset --hard HEAD~1"); risk != configuration.RiskLevelHigh {
		t.Errorf("git reset --hard risk = %q, want %q", risk, configuration.RiskLevelHigh)
	}

	// High risk: force flag escalation
	if risk := agent.EvaluateOperationRisk("git status --force"); risk != configuration.RiskLevelHigh {
		t.Errorf("force flag escalation risk = %q, want %q", risk, configuration.RiskLevelHigh)
	}
}

func TestEvaluateOperationRisk_NilConfigReturnsLow(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("some_persona")
	agent.configManager = nil

	risk := agent.EvaluateOperationRisk("rm -rf /tmp")
	if risk != configuration.RiskLevelLow {
		t.Errorf("EvaluateOperationRisk with nil configManager = %q, want %q", risk, configuration.RiskLevelLow)
	}
}

// =============================================================================
// isGitWriteAllowed — capability-driven authorization
// =============================================================================
//
// git-write authorization is driven solely by CapabilityGitWrite. A persona
// must explicitly carry CapabilityGitWrite to clear the gate.

func TestIsGitWriteAllowed_CapabilityBearingNonOrchestratorReturnsTrue(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SubagentTypes["test_with_git_cap"] = configuration.SubagentType{
			ID:           "test_with_git_cap",
			Name:         "With Git Capability",
			Enabled:      true,
			Capabilities: []string{personas.CapabilityGitWrite},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	agent.state.SetActivePersona("test_with_git_cap")

	if !agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed=true for non-orchestrator persona declaring CapabilityGitWrite")
	}
}

func TestIsGitWriteAllowed_WithoutCapabilityReturnsFalse(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SubagentTypes["test_no_cap"] = configuration.SubagentType{
			ID:      "test_no_cap",
			Name:    "Without Git Capability",
			Enabled: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	agent.state.SetActivePersona("test_no_cap")

	if agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed=false for persona without CapabilityGitWrite")
	}
}

func TestIsGitWriteAllowed_AutoApproveRulesAloneNoLongerGrants(t *testing.T) {
	// Regression for the rule-sniffing removal: declaring git_commit in
	// AutoApproveRules.MediumRiskOps used to imply git-write capability.
	// Post-B, only an explicit Capabilities entry counts.
	agent := newTestAgent(t)
	defer agent.Shutdown()

	customRules := configuration.AutoApproveRules{
		MediumRiskOps: []string{"git_commit", "git_push"},
	}
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SubagentTypes["test_rules_only"] = configuration.SubagentType{
			ID:               "test_rules_only",
			Name:             "Rules Only",
			Enabled:          true,
			AutoApproveRules: &customRules,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	agent.state.SetActivePersona("test_rules_only")

	if agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed=false: AutoApproveRules entries no longer imply capability")
	}
}

func TestIsGitWriteAllowed_EmptyPersonaReturnsFalse(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("")

	if agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed=false with no active persona")
	}
}

func TestIsGitWriteAllowed_NilConfigReturnsFalse(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("orchestrator")
	agent.configManager = nil

	if agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed=false with nil configManager")
	}
}

// =============================================================================
// isGitWriteAllowed for orchestrator persona
// =============================================================================

func TestIsOrchestratorGitWriteAllowed_OrchestratorPersonaReturnsTrue(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("orchestrator")

	// Orchestrator has CapabilityGitWrite in the catalog; no config flag needed.
	if !agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed to return true for orchestrator (has CapabilityGitWrite)")
	}
}

// SP-050: legacy "repo_orchestrator" ID is an alias for "orchestrator";
// ApplyPersona must canonicalize it so downstream gates see one name.
func TestApplyPersona_RepoOrchestratorAliasResolvesToOrchestrator(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.ApplyPersona("repo_orchestrator"); err != nil {
		t.Fatalf("ApplyPersona(repo_orchestrator) failed: %v", err)
	}

	if got := agent.GetActivePersona(); got != "orchestrator" {
		t.Errorf("activePersona after alias apply = %q, want %q", got, "orchestrator")
	}
}

// Renamed-persona alias coverage: code_reviewer → reviewer and
// executive_assistant → coordinator must canonicalize cleanly on apply.
func TestApplyPersona_CodeReviewerAliasResolvesToReviewer(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.ApplyPersona("code_reviewer"); err != nil {
		t.Fatalf("ApplyPersona(code_reviewer) failed: %v", err)
	}
	if got := agent.GetActivePersona(); got != "reviewer" {
		t.Errorf("activePersona after alias apply = %q, want %q", got, "reviewer")
	}
}

func TestApplyPersona_ExecutiveAssistantAliasResolvesToCoordinator(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.ApplyPersona("executive_assistant"); err != nil {
		t.Fatalf("ApplyPersona(executive_assistant) failed: %v", err)
	}
	if got := agent.GetActivePersona(); got != "coordinator" {
		t.Errorf("activePersona after alias apply = %q, want %q", got, "coordinator")
	}
}

// SP-050: ApplyPersona("orchestrator") always appends the git-policy markdown
// so the model knows about the commit tool, staging rules, and blocked
// shell-side git ops. The append is unconditional — the policy text is useful
// guidance regardless of runtime permissions.
func TestApplyPersona_OrchestratorGitPolicyAppended(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.ApplyPersona("orchestrator"); err != nil {
		t.Fatalf("ApplyPersona(orchestrator) failed: %v", err)
	}

	prompt := agent.GetSystemPrompt()
	// Use a marker phrase unique to the embedded persona-append file so we
	// distinguish it from any "git" content that already lives in the base
	// system prompt.
	if !strings.Contains(prompt, "ALWAYS use the 'commit' tool for all commits") {
		t.Error("expected orchestrator git policy append in system prompt")
	}
}

func TestIsOrchestratorGitWriteAllowed_NonOrchestratorNonEAPersonaReturnsFalse(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("coder")

	if agent.isGitWriteAllowed() {
		t.Error("expected isGitWriteAllowed to return false for non-orchestrator, non-EA persona")
	}
}

// --- canSpawnNonDelegatable tests ---
//
// Post-A: spawn authority is declarative. The coordinator's catalog entry
// declares CanSpawnNonDelegatable=["orchestrator"], replacing the previous
// hasEASpawnAuthority hardcoded check + the AutoApproveRules rule-sniffing.

func TestCanSpawnNonDelegatable_CoordinatorCanSpawnOrchestrator(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("coordinator")

	if !agent.canSpawnNonDelegatable("orchestrator") {
		t.Error("expected coordinator to be able to spawn orchestrator (catalog declares it)")
	}
}

func TestCanSpawnNonDelegatable_CoordinatorCannotSpawnSelf(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("coordinator")

	if agent.canSpawnNonDelegatable("coordinator") {
		t.Error("expected coordinator NOT to be able to spawn another coordinator (not in its allow list)")
	}
}

func TestCanSpawnNonDelegatable_CoderCannotSpawnOrchestrator(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("coder")

	if agent.canSpawnNonDelegatable("orchestrator") {
		t.Error("expected coder NOT to be able to spawn orchestrator")
	}
}

func TestCanSpawnNonDelegatable_OrchestratorCannotSpawnNonDelegatable(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.state.SetActivePersona("orchestrator")

	// Orchestrator has no CanSpawnNonDelegatable entries — it can only spawn
	// the regular delegatable specialists.
	if agent.canSpawnNonDelegatable("coordinator") {
		t.Error("expected orchestrator NOT to be able to spawn coordinator")
	}
}

func TestCanSpawnNonDelegatable_AutoApproveRulesDoNotGrantAuthority(t *testing.T) {
	// Regression for rule-sniffing removal: declaring "subagent_spawn" in
	// AutoApproveRules used to imply spawn authority. Post-A only an explicit
	// CanSpawnNonDelegatable list grants it.
	agent := newTestAgent(t)
	defer agent.Shutdown()

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SubagentTypes["test_rule_only"] = configuration.SubagentType{
			ID:          "test_rule_only",
			Name:        "Rule Only",
			Enabled:     true,
			Delegatable: false,
			AutoApproveRules: &configuration.AutoApproveRules{
				MediumRiskOps: []string{"subagent_spawn"},
			},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	agent.state.SetActivePersona("test_rule_only")

	if agent.canSpawnNonDelegatable("orchestrator") {
		t.Error("AutoApproveRules entries should no longer imply spawn authority")
	}
}
