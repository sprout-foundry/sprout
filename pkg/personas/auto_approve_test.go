package personas

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// expectedDefaultAutoApproveRules holds the canonical auto-approve rules that match
// configuration.DefaultAutoApproveRules(). This is duplicated here to avoid an
// import cycle (personas <- configuration <- personas).
var expectedDefaultAutoApproveRules = AutoApproveRules{
	LowRiskOps: []string{
		"git_add", "git_status", "git_log", "git_diff",
		"read_file",
	},
	MediumRiskOps: []string{
		"git_commit", "git_push", "git_pull", "git_fetch",
		"write_file", "edit_file", "shell_command",
		"rm_command", "docker",
		"subagent_spawn", "cross_directory",
	},
	HighRiskNever: []string{
		"force_flag", "rm_recursive", "git_reset_hard",
		"git_clean", "docker_prune", "git_push_force",
		"git_checkout", "git_switch", "git_restore", "git_branch_delete",
	},
}

func TestExecutiveAssistant_AutoApproveRules_LoadedFromJSON(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("failed to load default definitions: %v", err)
	}

	ea, exists := definitions["executive_assistant"]
	if !exists {
		t.Fatal("expected executive_assistant in default definitions")
	}

	// auto_approve_rules must be non-nil after loading from JSON
	if ea.AutoApproveRules == nil {
		t.Fatal("expected executive_assistant to have auto_approve_rules loaded from JSON")
	}
}

func TestExecutiveAssistant_AutoApproveRules_MatchesDefaultAutoApproveRules(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("failed to load default definitions: %v", err)
	}

	ea, exists := definitions["executive_assistant"]
	if !exists {
		t.Fatal("expected executive_assistant in default definitions")
	}

	if ea.AutoApproveRules == nil {
		t.Fatal("expected auto_approve_rules to be non-nil")
	}

	if !reflect.DeepEqual(ea.AutoApproveRules.LowRiskOps, expectedDefaultAutoApproveRules.LowRiskOps) {
		t.Errorf("LowRiskOps mismatch:\ngot:  %v\nwant: %v", ea.AutoApproveRules.LowRiskOps, expectedDefaultAutoApproveRules.LowRiskOps)
	}

	if !reflect.DeepEqual(ea.AutoApproveRules.MediumRiskOps, expectedDefaultAutoApproveRules.MediumRiskOps) {
		t.Errorf("MediumRiskOps mismatch:\ngot:  %v\nwant: %v", ea.AutoApproveRules.MediumRiskOps, expectedDefaultAutoApproveRules.MediumRiskOps)
	}

	if !reflect.DeepEqual(ea.AutoApproveRules.HighRiskNever, expectedDefaultAutoApproveRules.HighRiskNever) {
		t.Errorf("HighRiskNever mismatch:\ngot:  %v\nwant: %v", ea.AutoApproveRules.HighRiskNever, expectedDefaultAutoApproveRules.HighRiskNever)
	}
}

func TestExecutiveAssistant_AutoApproveRules_RiskCategoryCounts(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("failed to load default definitions: %v", err)
	}

	ea, exists := definitions["executive_assistant"]
	if !exists {
		t.Fatal("expected executive_assistant in default definitions")
	}

	if ea.AutoApproveRules == nil {
		t.Fatal("expected auto_approve_rules to be non-nil")
	}

	tests := []struct {
		name     string
		actual   int
		expected int
	}{
		{"low_risk", len(ea.AutoApproveRules.LowRiskOps), 5},
		{"medium_risk", len(ea.AutoApproveRules.MediumRiskOps), 11},
		{"high_risk_never", len(ea.AutoApproveRules.HighRiskNever), 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.actual != tc.expected {
				t.Errorf("expected %d entries in %s, got %d", tc.expected, tc.name, tc.actual)
			}
		})
	}
}

func TestExecutiveAssistant_AutoApproveRules_DirectJSONLoad(t *testing.T) {
	// Load the JSON directly to verify raw deserialization works
	data, err := os.ReadFile("configs/executive_assistant.json")
	if err != nil {
		t.Fatalf("failed to read executive_assistant.json: %v", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("failed to parse executive_assistant.json: %v", err)
	}

	if len(catalog.Personas) != 1 {
		t.Fatalf("expected 1 persona in catalog, got %d", len(catalog.Personas))
	}

	ea := catalog.Personas[0]
	if ea.AutoApproveRules == nil {
		t.Fatal("expected auto_approve_rules to be present in raw JSON parse")
	}

	if len(ea.AutoApproveRules.LowRiskOps) == 0 {
		t.Fatal("expected non-empty low_risk from JSON")
	}
	if len(ea.AutoApproveRules.MediumRiskOps) == 0 {
		t.Fatal("expected non-empty medium_risk from JSON")
	}
	if len(ea.AutoApproveRules.HighRiskNever) == 0 {
		t.Fatal("expected non-empty high_risk_never from JSON")
	}
}
