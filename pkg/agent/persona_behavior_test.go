package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestNormalizeAgentPersonaID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Orchestrator", "orchestrator"},
		{"Web-Scraper", "web_scraper"},
		{"CODE_REVIEWER", "code_reviewer"},
		{"  coder  ", "coder"},
		{"", ""},
		{"Test-Persona-ID", "test_persona_id"},
		{"web_scraper", "web_scraper"},
		{"UPPER-CASE-HYPHEN", "upper_case_hyphen"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeAgentPersonaID(tt.input)
			if got != tt.want {
				t.Errorf("normalizeAgentPersonaID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClearActivePersona(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Set a custom system prompt to verify it's restored.
	basePrompt := "You are the base assistant."
	agent.baseSystemPrompt = basePrompt

	// Activate a persona by overriding the system prompt.
	agent.activePersona = "coder"
	agent.systemPrompt = "You are a focused coder."

	agent.ClearActivePersona()

	if got := agent.GetActivePersona(); got != "" {
		t.Errorf("GetActivePersona after ClearActivePersona = %q, want empty", got)
	}
	if agent.activePersona != "" {
		t.Errorf("activePersona field = %q, want empty", agent.activePersona)
	}

	// The system prompt should be restored to baseSystemPrompt.
	if agent.systemPrompt != basePrompt {
		t.Errorf("systemPrompt after ClearActivePersona = %q, want %q", agent.systemPrompt, basePrompt)
	}
}

func TestClearActivePersonaNoBase(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// When baseSystemPrompt is empty/witespace, ClearActivePersona should
	// not overwrite the system prompt (the field stays unchanged).
	agent.activePersona = "coder"
	agent.baseSystemPrompt = ""
	existingPrompt := agent.systemPrompt

	agent.ClearActivePersona()

	if agent.activePersona != "" {
		t.Errorf("activePersona = %q, want empty", agent.activePersona)
	}
	// systemPrompt should remain unchanged when baseSystemPrompt is empty.
	if agent.systemPrompt != existingPrompt {
		t.Errorf("systemPrompt changed unexpectedly after ClearActivePersona with empty base")
	}
}

func TestGetAvailablePersonaIDs(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// The default config loads embedded persona definitions (at least "orchestrator").
	defaultIDs := agent.GetAvailablePersonaIDs()
	if len(defaultIDs) == 0 {
		t.Fatal("expected at least one persona from default config")
	}
	t.Logf("default persona IDs: %v", defaultIDs)

	// Add custom persona via UpdateConfigNoSave.
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes["test_unique_persona_xyz"] = configuration.SubagentType{Enabled: true}
		cfg.SubagentTypes["disabled_role"] = configuration.SubagentType{Enabled: false}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	ids := agent.GetAvailablePersonaIDs()
	t.Logf("updated persona IDs: %v", ids)

	// New custom persona should be present.
	found := false
	for _, id := range ids {
		if id == "test_unique_persona_xyz" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test_unique_persona_xyz should be in available IDs")
	}

	// Disabled persona should never be included.
	for _, id := range ids {
		if id == "disabled_role" {
			t.Error("disabled persona should not be in available IDs")
		}
	}

	// Verify results are sorted.
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Errorf("persona IDs not sorted: %q > %q", ids[i-1], ids[i])
		}
	}
}

func TestGetAvailablePersonaIDsNilConfig(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.configManager = nil
	ids := agent.GetAvailablePersonaIDs()
	if ids != nil {
		t.Errorf("GetAvailablePersonaIDs with nil configManager = %v, want nil", ids)
	}
}

func TestGetAvailableToolNames(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	names := agent.GetAvailableToolNames()

	if len(names) == 0 {
		t.Fatal("GetAvailableToolNames returned empty list, expected at least one tool")
	}

	// Verify known tools are present.
	known := map[string]bool{
		"shell_command":          false,
		"read_file":              false,
		"write_file":             false,
		"edit_file":              false,
		"write_structured_file":  false,
		"search_files":           false,
	}

	for _, name := range names {
		if _, exists := known[name]; exists {
			known[name] = true
		}
	}

	for tool, found := range known {
		if !found {
			t.Errorf("expected tool %q in GetAvailableToolNames", tool)
		}
	}

	// Verify tools are sorted.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("tools not sorted: %q > %q", names[i-1], names[i])
		}
	}
}

func TestGetAvailableToolNamesNoDuplicates(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	names := agent.GetAvailableToolNames()

	seen := make(map[string]int)
	for _, name := range names {
		seen[name]++
		if seen[name] > 1 {
			t.Errorf("duplicate tool name: %q", name)
		}
	}
}
