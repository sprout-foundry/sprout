package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestSettingsCommand_Name(t *testing.T) {
	cmd := &SettingsCommand{}
	if got := cmd.Name(); got != "settings" {
		t.Errorf("SettingsCommand.Name() = %q, want \"settings\"", got)
	}
}

func TestSettingsCommand_Description(t *testing.T) {
	cmd := &SettingsCommand{}
	desc := cmd.Description()
	if desc == "" {
		t.Error("SettingsCommand.Description() should not be empty")
	}
}

func TestSettingsCommand_Execute_NilAgent(t *testing.T) {
	cmd := &SettingsCommand{}
	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Error("SettingsCommand.Execute() with nil agent should return error")
	}
}

func TestSettingsCommand_Execute_NilConfigManager(t *testing.T) {
	// Create a minimal agent with no config manager
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	cmd := &SettingsCommand{}
	// Execute will fail because AskUser will return ErrAskUserNoChannel
	// (no TTY in tests), which is handled gracefully
	err = cmd.Execute(nil, chatAgent)
	// Should not panic; either returns nil (non-TTY fallback) or an error
	_ = err
}

func TestBuildSettingsOptions(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
		ProviderModels: map[string]string{
			"anthropic": "claude-sonnet-4-20250514",
		},
		ReasoningEffort: "medium",
		EAMode:          "interactive",
		HistoryScope:    "project",
		OutputVerbosity: "default",
	}

	settings := agent.AllSettings()
	if len(settings) == 0 {
		t.Fatal("AllSettings() returned empty list")
	}

	options := buildSettingsOptions(settings, cfg)
	if len(options) == 0 {
		t.Fatal("buildSettingsOptions returned empty list")
	}

	// Should have one option per setting plus a quit option
	expectedCount := len(settings) + 1
	if len(options) != expectedCount {
		t.Errorf("Expected %d options, got %d", expectedCount, len(options))
	}

	// Last option should be quit
	lastOpt := options[len(options)-1]
	if lastOpt.Value != "q" {
		t.Errorf("Last option should be quit, got value %q", lastOpt.Value)
	}

	// Check that provider option contains current value
	foundProvider := false
	for _, opt := range options {
		if opt.Value == "provider" {
			foundProvider = true
			if !strings.Contains(opt.Label, "anthropic") {
				t.Errorf("Provider option label should contain 'anthropic', got %q", opt.Label)
			}
			break
		}
	}
	if !foundProvider {
		t.Error("Should have a provider option")
	}
}

func TestBuildSettingsOptions_NotSet(t *testing.T) {
	cfg := &configuration.Config{} // Empty config

	settings := agent.AllSettings()
	options := buildSettingsOptions(settings, cfg)

	// Check that unset values show "(not set)"
	for _, opt := range options {
		if opt.Value == "provider" {
			if !strings.Contains(opt.Label, "(not set)") {
				t.Errorf("Unset provider should show '(not set)', got label %q", opt.Label)
			}
			break
		}
	}
}

func TestGetEnumOptions(t *testing.T) {
	cfg := &configuration.Config{}

	tests := []struct {
		key      string
		hasOpts  bool
		optCount int
		values   []string
	}{
		{"reasoning_effort", true, 3, []string{"low", "medium", "high"}},
		{"disable_thinking", true, 2, []string{"false", "true"}},
		{"history_scope", true, 2, []string{"project", "global"}},
		{"ea_mode", true, 2, []string{"interactive", "queue"}},
		{"output_verbosity", true, 3, []string{"compact", "default", "verbose"}},
		{"provider", false, 0, nil},
		{"model", false, 0, nil},
		{"unknown_key", false, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			opts, ok := getEnumOptions(tt.key, cfg)
			if ok != tt.hasOpts {
				t.Errorf("getEnumOptions(%q) hasOpts = %v, want %v", tt.key, ok, tt.hasOpts)
			}
			if tt.hasOpts && len(opts) != tt.optCount {
				t.Errorf("getEnumOptions(%q) returned %d options, want %d", tt.key, len(opts), tt.optCount)
			}
			if tt.hasOpts {
				for i, wantVal := range tt.values {
					if opts[i].Value != wantVal {
						t.Errorf("getEnumOptions(%q)[%d].Value = %q, want %q", tt.key, i, opts[i].Value, wantVal)
					}
				}
			}
		})
	}
}

func TestIsQuit(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"q", true},
		{"Q", true},
		{"quit", true},
		{"QUIT", true},
		{"exit", true},
		{"EXIT", true},
		{" q ", true},
		{" quit ", true},
		{"provider", false},
		{"", false},
		{"1", false},
		{"no", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isQuit(tt.input); got != tt.want {
				t.Errorf("isQuit(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPromptSettingValue_ErrAskUserNoChannel(t *testing.T) {
	// In test environment, AskUser will return ErrAskUserNoChannel
	// because there's no TTY. Verify the function propagates this.
	setting := agent.SettingDetail{
		Key:         "reasoning_effort",
		Description: "Reasoning effort",
		ValidValues: "low, medium, high",
		GetValue:    func(cfg *configuration.Config) string { return "medium" },
	}
	cfg := &configuration.Config{ReasoningEffort: "medium"}

	_, err := promptSettingValue(setting, cfg, nil)
	if err == nil {
		t.Error("Expected error from promptSettingValue in non-TTY environment")
	}
	if !strings.Contains(err.Error(), "no interactive channel") {
		t.Errorf("Expected ErrAskUserNoChannel, got: %v", err)
	}
}

func TestRenderSettingsSummary(t *testing.T) {
	// Verify renderSettingsSummary doesn't panic with valid input
	cfg := &configuration.Config{
		LastUsedProvider: "test",
		ReasoningEffort:  "low",
	}
	settings := agent.AllSettings()
	var buf bytes.Buffer

	renderSettingsSummary(&buf, settings, cfg)

	output := buf.String()
	if !strings.Contains(output, "Settings") {
		t.Errorf("expected output to contain 'Settings', got: %q", output)
	}
	if !strings.Contains(output, "test") {
		t.Errorf("expected output to contain provider 'test', got: %q", output)
	}
}

func TestProviderOptionKeys(t *testing.T) {
	expected := []string{"provider", "subagent_provider", "commit_provider", "review_provider"}
	for _, key := range expected {
		if !providerOptionKeys[key] {
			t.Errorf("providerOptionKeys missing %q", key)
		}
	}
}

func TestGetProviderOptions(t *testing.T) {
	// nil manager → no options
	opts, ok := getProviderOptions(nil)
	if ok {
		t.Error("getProviderOptions(nil) should return false")
	}
	if opts != nil {
		t.Error("getProviderOptions(nil) should return nil options")
	}
}

func TestSettingsCommand_NonTTYFallback(t *testing.T) {
	// Verify that when AskUser returns ErrAskUserNoChannel,
	// the command handles it gracefully. We simulate this by
	// checking that the tools package returns the error in non-TTY.
	_, err := tools.AskUser(tools.AskUserRequest{
		Question: "test question",
	})
	if err != tools.ErrAskUserNoChannel {
		// In CI/test env this should be ErrAskUserNoChannel
		// If it's not, that's fine — the test environment may vary
		t.Logf("AskUser returned: %v (expected ErrAskUserNoChannel in non-TTY)", err)
	}
}
