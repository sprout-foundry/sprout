package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// mockPrompter
// ---------------------------------------------------------------------------

// mockPrompter returns pre-recorded responses in order, then errors on
// unexpected calls.
type mockPrompter struct {
	responses []string
	errors    []error // per-call error overrides (nil = no error)
	calls     []string // records prompt text for each call
	count     int
}

func (m *mockPrompter) Prompt(prompt string) (string, error) {
	m.calls = append(m.calls, prompt)
	if m.count >= len(m.responses) {
		return "", fmt.Errorf("unexpected prompt call #%d: %s", m.count+1, prompt)
	}
	resp := m.responses[m.count]
	var err error
	if m.count < len(m.errors) {
		err = m.errors[m.count]
	}
	m.count++
	return resp, err
}

// ---------------------------------------------------------------------------
// setup helpers
// ---------------------------------------------------------------------------

// newTestRoleManager creates a RoleManager backed by a temp directory.
// When workspaceDir is left empty, Save() writes to globalDir.
func newTestRoleManager(t *testing.T) *configuration.RoleManager {
	t.Helper()
	tmpDir := t.TempDir()
	return configuration.NewRoleManager(tmpDir, "")
}

// newTestExtendAgent returns a minimal *agent.Agent sufficient to pass
// Execute's nil-agent check.
func newTestExtendAgent(t *testing.T) *agent.Agent {
	t.Helper()
	return &agent.Agent{}
}

// ---------------------------------------------------------------------------
// Registration & metadata
// ---------------------------------------------------------------------------

func TestExtendCommand_Registration(t *testing.T) {
	t.Parallel()
	reg := NewCommandRegistry()
	cmd, ok := reg.GetCommand("extend")
	if !ok {
		t.Fatal("expected 'extend' command to be registered")
	}
	if _, ok := cmd.(*ExtendCommand); !ok {
		t.Fatalf("expected *ExtendCommand, got %T", cmd)
	}
}

func TestExtendCommand_Name(t *testing.T) {
	t.Parallel()
	cmd := &ExtendCommand{}
	if got := cmd.Name(); got != "extend" {
		t.Errorf("Name() = %q, want \"extend\"", got)
	}
}

func TestExtendCommand_Description(t *testing.T) {
	t.Parallel()
	cmd := &ExtendCommand{}
	got := cmd.Description()
	want := "Create or modify agent roles with guided configuration"
	if got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Nil agent
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_NilAgent(t *testing.T) {
	t.Parallel()
	cmd := &ExtendCommand{}
	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
	if !strings.Contains(err.Error(), "[extend] agent not available") {
		t.Errorf("error = %q, want it to contain \"[extend] agent not available\"", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Happy path – full question flow then confirm
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_HappyPath(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"test-role",       // Q1 name
			"A test role",    // Q2 description
			"You are helpful", // Q3 system prompt
			"read_file,write_file", // Q4 tools
			"openai",         // Q5 provider
			"gpt-4",          // Q6 model
			"10",             // Q7 max iterations
			"y",              // confirm
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify the saved role on disk
	cfg, err := rm.Resolve("test-role")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Name != "test-role" {
		t.Errorf("Name = %q, want \"test-role\"", cfg.Name)
	}
	if cfg.Description != "A test role" {
		t.Errorf("Description = %q, want \"A test role\"", cfg.Description)
	}
	if cfg.SystemPrompt != "You are helpful" {
		t.Errorf("SystemPrompt = %q, want \"You are helpful\"", cfg.SystemPrompt)
	}
	if len(cfg.Tools.AllowedTools) != 2 {
		t.Fatalf("AllowedTools len = %d, want 2", len(cfg.Tools.AllowedTools))
	}
	if cfg.Tools.AllowedTools[0] != "read_file" {
		t.Errorf("AllowedTools[0] = %q, want \"read_file\"", cfg.Tools.AllowedTools[0])
	}
	if cfg.Tools.AllowedTools[1] != "write_file" {
		t.Errorf("AllowedTools[1] = %q, want \"write_file\"", cfg.Tools.AllowedTools[1])
	}
	if cfg.Provider != "openai" {
		t.Errorf("Provider = %q, want \"openai\"", cfg.Provider)
	}
	if cfg.Model != "gpt-4" {
		t.Errorf("Model = %q, want \"gpt-4\"", cfg.Model)
	}
	if cfg.Constraints.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", cfg.Constraints.MaxIterations)
	}
}

// ---------------------------------------------------------------------------
// Happy path – all defaults
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_AllDefaults(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"my-role",   // Q1 name
			"desc",      // Q2 description
			"sys",       // Q3 system prompt
			"all",       // Q4 tools = all → empty
			"default",   // Q5 provider → empty
			"default",   // Q6 model → empty
			"default",   // Q7 max iterations → 0
			"yes",       // confirm
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := rm.Resolve("my-role")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// "all" for tools → nil/empty AllowedTools
	if len(cfg.Tools.AllowedTools) != 0 {
		t.Errorf("AllowedTools = %v, want empty", cfg.Tools.AllowedTools)
	}
	// "default" for provider/model → empty string
	if cfg.Provider != "" {
		t.Errorf("Provider = %q, want empty", cfg.Provider)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty", cfg.Model)
	}
	// "default" for max iterations → 0
	if cfg.Constraints.MaxIterations != 0 {
		t.Errorf("MaxIterations = %d, want 0", cfg.Constraints.MaxIterations)
	}
}

// ---------------------------------------------------------------------------
// Happy path – mixed defaults and custom values
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_MixedDefaults(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"mixed",     // Q1 name
			"desc",      // Q2 description
			"",          // Q3 system prompt (empty)
			"",          // Q4 tools (empty → all)
			"",          // Q5 provider (empty, stays empty)
			"some-model",// Q6 model (custom)
			"",          // Q7 max iterations (empty → 0)
			"Y",         // confirm (uppercase)
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := rm.Resolve("mixed")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Model != "some-model" {
		t.Errorf("Model = %q, want \"some-model\"", cfg.Model)
	}
	if len(cfg.Tools.AllowedTools) != 0 {
		t.Errorf("AllowedTools = %v, want empty", cfg.Tools.AllowedTools)
	}
}

// ---------------------------------------------------------------------------
// Cancel scenarios
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_CancelEmptyRoleName(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{""}, // Q1 empty → cancel
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() should return nil on cancel, got: %v", err)
	}

	// Verify nothing was saved
	if rm.Exists("test") {
		t.Error("expected no role to be saved after cancel")
	}
}

func TestExtendCommand_Execute_CancelAtDescription(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"cancel-desc",  // Q1 name (valid)
			"",             // Q2 description (empty, not treated as cancel by code)
			"sys",          // Q3
			"all",          // Q4
			"default",      // Q5
			"default",      // Q6
			"default",      // Q7
			"n",            // confirm = reject → no save
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	// The code does NOT cancel on empty description (only on empty name).
	// But we reject at confirmation, so no save happens.
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() should return nil on rejected confirm, got: %v", err)
	}

	// Verify nothing was saved (rejected confirmation)
	roles, _ := rm.List()
	if len(roles) > 0 {
		t.Errorf("expected no roles saved, got %d", len(roles))
	}
}

// ---------------------------------------------------------------------------
// Confirmation rejection
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_RejectConfirmation(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"rejected",   // Q1
			"desc",       // Q2
			"sys",        // Q3
			"all",        // Q4
			"default",    // Q5
			"default",    // Q6
			"default",    // Q7
			"no",         // confirm = "no" → reject
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() should return nil on rejected confirm, got: %v", err)
	}

	// Verify nothing was saved
	roles, _ := rm.List()
	if len(roles) > 0 {
		t.Errorf("expected no roles after rejection, got %d", len(roles))
	}
}

func TestExtendCommand_Execute_RejectConfirmationWithN(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"reject-n", "desc", "sys", "all",
			"default", "default", "default", "n",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if rm.Exists("reject-n") {
		t.Error("expected role NOT to be saved after 'n' confirmation")
	}
}

func TestExtendCommand_Execute_RejectConfirmationWithRandomText(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"random-reject", "desc", "sys", "all",
			"default", "default", "default", "maybe",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if rm.Exists("random-reject") {
		t.Error("expected role NOT to be saved after non-y/yes confirmation")
	}
}

// ---------------------------------------------------------------------------
// Input errors from prompter
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_PrompterError(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{"fail"},
		errors:    []error{fmt.Errorf("read failed")},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err == nil {
		t.Fatal("expected error from prompter, got nil")
	}
	if !strings.Contains(err.Error(), "[extend] input error") {
		t.Errorf("error = %q, want it to contain \"[extend] input error\"", err.Error())
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Errorf("error = %q, want it to contain \"read failed\"", err.Error())
	}
}

func TestExtendCommand_Execute_PrompterErrorAtQuestion7(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"q7-fail", "desc", "sys", "all", "prov", "model", "bad", "y",
		},
		errors: []error{
			nil, nil, nil, nil, nil, nil, fmt.Errorf("scanner error"), nil,
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err == nil {
		t.Fatal("expected error from prompter at Q7, got nil")
	}
	if !strings.Contains(err.Error(), "[extend] input error") {
		t.Errorf("error = %q, want it to contain \"[extend] input error\"", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Invalid role name
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_InvalidRoleName(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "path traversal",
			input:   "../etc/passwd",
			wantErr: "[extend] invalid role name",
		},
		{
			name:    "spaces",
			input:   "my role",
			wantErr: "[extend] invalid role name",
		},
		{
			name:    "special chars",
			input:   "role@name",
			wantErr: "[extend] invalid role name",
		},
		{
			name:    "forward slash",
			input:   "role/name",
			wantErr: "[extend] invalid role name",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &mockPrompter{responses: []string{tt.input}}

			cmd := &ExtendCommand{
				roleManager: rm,
				prompter:    p,
			}
			err := cmd.Execute(nil, newTestExtendAgent(t))
			if err == nil {
				t.Fatalf("expected error for invalid role name %q, got nil", tt.input)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Invalid max iterations
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_InvalidMaxIterations(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"iter-test", "desc", "sys", "all",
			"default", "default", "not-a-number",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err == nil {
		t.Fatal("expected error for non-numeric max iterations")
	}
	if !strings.Contains(err.Error(), "[extend] invalid max iterations") {
		t.Errorf("error = %q, want it to contain \"[extend] invalid max iterations\"", err.Error())
	}
}

func TestExtendCommand_Execute_NegativeMaxIterations(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"neg-iter", "desc", "sys", "all",
			"default", "default", "-5",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err == nil {
		t.Fatal("expected error for negative max iterations")
	}
	if !strings.Contains(err.Error(), "[extend] max iterations must be non-negative") {
		t.Errorf("error = %q, want it to contain \"[extend] max iterations must be non-negative\"", err.Error())
	}
	// Verify nothing was saved
	if rm.Exists("neg-iter") {
		t.Error("expected role NOT to be saved after negative max iterations error")
	}
}

func TestExtendCommand_Execute_DuplicateRoleOverwriteYes(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	// Create the role first
	existingRole := configuration.RoleConfig{
		Name:        "dup-role",
		Description: "original description",
	}
	if err := rm.Save(existingRole, ""); err != nil {
		t.Fatalf("failed to create existing role: %v", err)
	}

	p := &mockPrompter{
		responses: []string{
			"dup-role",        // Q1 name (already exists)
			"new description", // Q2
			"new sys prompt",  // Q3
			"all",             // Q4
			"default",         // Q5
			"default",         // Q6
			"default",         // Q7
			"y",               // overwrite confirm
			"y",               // save confirm
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify the role was overwritten
	cfg, err := rm.Resolve("dup-role")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Description != "new description" {
		t.Errorf("Description = %q, want \"new description\"", cfg.Description)
	}
	if cfg.SystemPrompt != "new sys prompt" {
		t.Errorf("SystemPrompt = %q, want \"new sys prompt\"", cfg.SystemPrompt)
	}
}

func TestExtendCommand_Execute_DuplicateRoleOverwriteNo(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	// Create the role first
	existingRole := configuration.RoleConfig{
		Name:        "dup-role-no",
		Description: "original description",
	}
	if err := rm.Save(existingRole, ""); err != nil {
		t.Fatalf("failed to create existing role: %v", err)
	}

	p := &mockPrompter{
		responses: []string{
			"dup-role-no",       // Q1 name (already exists)
			"new description",   // Q2
			"new sys prompt",    // Q3
			"all",               // Q4
			"default",           // Q5
			"default",           // Q6
			"default",           // Q7
			"n",                 // overwrite confirm = no
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() should return nil on rejected overwrite, got: %v", err)
	}

	// Verify the role was NOT overwritten
	cfg, err := rm.Resolve("dup-role-no")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Description != "original description" {
		t.Errorf("Description = %q, want \"original description\" (should not have been overwritten)", cfg.Description)
	}
}

// ---------------------------------------------------------------------------
// Tools parsing edge cases
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_ToolsParsing(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	tests := []struct {
		name         string
		toolsInput   string
		wantTools    []string
	}{
		{
			name:       "single_tool",
			toolsInput: "read_file",
			wantTools:  []string{"read_file"},
		},
		{
			name:       "comma_separated_with_spaces",
			toolsInput: "read_file, write_file, shell_command",
			wantTools:  []string{"read_file", "write_file", "shell_command"},
		},
		{
			name:       "all_lowercase",
			toolsInput: "all",
			wantTools:  nil,
		},
		{
			name:       "ALL_uppercase",
			toolsInput: "ALL",
			wantTools:  nil,
		},
		{
			name:       "All_mixed_case",
			toolsInput: "All",
			wantTools:  nil,
		},
		{
			name:       "empty_string",
			toolsInput: "",
			wantTools:  nil,
		},
		{
			name:       "extra_commas_trimmed",
			toolsInput: "a,,b,  ,c",
			wantTools:  []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &mockPrompter{
				responses: []string{
					"tools_" + tt.name, "desc", "sys", tt.toolsInput,
					"default", "default", "default", "y",
				},
			}

			cmd := &ExtendCommand{
				roleManager: rm,
				prompter:    p,
			}
			err := cmd.Execute(nil, newTestExtendAgent(t))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			cfg, err := rm.Resolve("tools_" + tt.name)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			// Compare tool lists
			if len(cfg.Tools.AllowedTools) != len(tt.wantTools) {
				t.Errorf("AllowedTools = %v (len %d), want %v (len %d)",
					cfg.Tools.AllowedTools, len(cfg.Tools.AllowedTools),
					tt.wantTools, len(tt.wantTools))
				return
			}
			for i, want := range tt.wantTools {
				if cfg.Tools.AllowedTools[i] != want {
					t.Errorf("AllowedTools[%d] = %q, want %q", i, cfg.Tools.AllowedTools[i], want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Provider / Model case insensitivity for "default"
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_ProviderDefaultCaseInsensitive(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	tests := []struct {
		name       string
		provider   string
		wantEmpty  bool
	}{
		{"default_lowercase", "default", true},
		{"DEFAULT_uppercase", "DEFAULT", true},
		{"Default_mixed", "Default", true},
		{"custom_provider", "anthropic", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &mockPrompter{
				responses: []string{
					"prov_" + tt.name, "desc", "sys", "all",
					tt.provider, "default", "default", "y",
				},
			}

			cmd := &ExtendCommand{
				roleManager: rm,
				prompter:    p,
			}
			err := cmd.Execute(nil, newTestExtendAgent(t))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			cfg, err := rm.Resolve("prov_" + tt.name)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if tt.wantEmpty && cfg.Provider != "" {
				t.Errorf("Provider = %q, want empty (default)", cfg.Provider)
			}
			if !tt.wantEmpty && cfg.Provider != tt.provider {
				t.Errorf("Provider = %q, want %q", cfg.Provider, tt.provider)
			}
		})
	}
}

func TestExtendCommand_Execute_ModelDefaultCaseInsensitive(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	tests := []struct {
		name       string
		model      string
		wantEmpty  bool
	}{
		{"default_lowercase", "default", true},
		{"DEFAULT_uppercase", "DEFAULT", true},
		{"custom_model", "claude-3", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &mockPrompter{
				responses: []string{
					"model_" + tt.name, "desc", "sys", "all",
					"default", tt.model, "default", "y",
				},
			}

			cmd := &ExtendCommand{
				roleManager: rm,
				prompter:    p,
			}
			err := cmd.Execute(nil, newTestExtendAgent(t))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			cfg, err := rm.Resolve("model_" + tt.name)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if tt.wantEmpty && cfg.Model != "" {
				t.Errorf("Model = %q, want empty (default)", cfg.Model)
			}
			if !tt.wantEmpty && cfg.Model != tt.model {
				t.Errorf("Model = %q, want %q", cfg.Model, tt.model)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Role name normalization (SaveRoleToFile lowercases names)
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_RoleNameNormalization(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"My-Role",   // Q1 – mixed case
			"desc",      // Q2
			"sys",       // Q3
			"all",       // Q4
			"default",   // Q5
			"default",   // Q6
			"default",   // Q7
			"y",         // confirm
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// SaveRoleToFile lowercases the name, so we look it up by the original
	// (RoleManager.Resolve also lowercases).
	cfg, err := rm.Resolve("My-Role")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Name != "My-Role" {
		t.Errorf("Name = %q, want \"My-Role\" (name is preserved as-is in config)", cfg.Name)
	}
	// The file on disk is lowercased
	entries, _ := os.ReadDir(rm.GlobalDir())
	if len(entries) != 1 {
		t.Fatalf("expected 1 role file, got %d", len(entries))
	}
	if entries[0].Name() != "my-role.yaml" {
		t.Errorf("filename = %q, want \"my-role.yaml\"", entries[0].Name())
	}
}

// ---------------------------------------------------------------------------
// Max iterations default / empty
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_MaxIterationsEmpty(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"empty-iter", "desc", "sys", "all",
			"default", "default", "", "y",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := rm.Resolve("empty-iter")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Constraints.MaxIterations != 0 {
		t.Errorf("MaxIterations = %d, want 0 (empty input)", cfg.Constraints.MaxIterations)
	}
}

func TestExtendCommand_Execute_MaxIterationsZero(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"zero-iter", "desc", "sys", "all",
			"default", "default", "0", "y",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := rm.Resolve("zero-iter")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Constraints.MaxIterations != 0 {
		t.Errorf("MaxIterations = %d, want 0", cfg.Constraints.MaxIterations)
	}
}

func TestExtendCommand_Execute_MaxIterationsLarge(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"big-iter", "desc", "sys", "all",
			"default", "default", "9999", "y",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := rm.Resolve("big-iter")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Constraints.MaxIterations != 9999 {
		t.Errorf("MaxIterations = %d, want 9999", cfg.Constraints.MaxIterations)
	}
}

// ---------------------------------------------------------------------------
// Prompter call order verification
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_PromptOrder(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"order-test", "desc", "sys", "all",
			"default", "default", "default", "y",
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	expectedPrompts := []string{
		"1/7 Role name (required)",
		"2/7 Description",
		"3/7 System prompt",
		"4/7 Allowed tools (comma-separated, or 'all')",
		"5/7 Provider override (or 'default')",
		"6/7 Model override (or 'default')",
		"7/7 Max iterations (or 'default')",
		"Save this role? (y/n)",
	}

	if len(p.calls) != len(expectedPrompts) {
		t.Fatalf("expected %d prompt calls, got %d", len(expectedPrompts), len(p.calls))
	}
	for i, want := range expectedPrompts {
		if p.calls[i] != want {
			t.Errorf("prompt[%d] = %q, want %q", i, p.calls[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// displayValue helper
// ---------------------------------------------------------------------------

func TestDisplayValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"", "(default)"},
		{"openai", "openai"},
		{"gpt-4", "gpt-4"},
		{"10", "10"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := displayValue(tt.input)
			if got != tt.want {
				t.Errorf("displayValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Role name with hyphens, underscores, dots (valid characters)
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_ValidRoleNames(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)

	validNames := []string{"my-role", "my_role", "my.role", "Role123"}
	for _, name := range validNames {
		name := name // capture
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := &mockPrompter{
				responses: []string{
					name, "desc", "sys", "all",
					"default", "default", "default", "y",
				},
			}

			cmd := &ExtendCommand{
				roleManager: rm,
				prompter:    p,
			}
			err := cmd.Execute(nil, newTestExtendAgent(t))
			if err != nil {
				t.Fatalf("Execute() error for valid name %q: %v", name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Multiple roles saved independently
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_MultipleRoles(t *testing.T) {
	// Cannot run in parallel — shares a single RoleManager across iterations.
	rm := newTestRoleManager(t)

	for _, name := range []string{"role-a", "role-b", "role-c"} {
		p := &mockPrompter{
			responses: []string{
				name, "desc for " + name, "sys", "all",
				"default", "default", "default", "y",
			},
		}
		cmd := &ExtendCommand{
			roleManager: rm,
			prompter:    p,
		}
		if err := cmd.Execute(nil, newTestExtendAgent(t)); err != nil {
			t.Fatalf("Execute(%q) error = %v", name, err)
		}
	}

	roles, err := rm.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}
}

// ---------------------------------------------------------------------------
// Saved YAML content verification
// ---------------------------------------------------------------------------

func TestExtendCommand_Execute_SavedYAMLContent(t *testing.T) {
	t.Parallel()
	rm := newTestRoleManager(t)
	p := &mockPrompter{
		responses: []string{
			"yaml-check",    // Q1
			"test description", // Q2
			"You are a bot", // Q3
			"read_file",     // Q4
			"anthropic",     // Q5
			"claude-3",      // Q6
			"25",            // Q7
			"y",             // confirm
		},
	}

	cmd := &ExtendCommand{
		roleManager: rm,
		prompter:    p,
	}
	err := cmd.Execute(nil, newTestExtendAgent(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Read raw file content to verify YAML structure
	fpath := filepath.Join(rm.GlobalDir(), "yaml-check.yaml")
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", fpath, err)
	}

	yamlStr := string(data)
	// Spot-check key values appear in the YAML
	mustContain := []string{
		"name: yaml-check",
		"description: test description",
		"system_prompt: You are a bot",
		"provider: anthropic",
		"model: claude-3",
	}
	for _, want := range mustContain {
		if !strings.Contains(yamlStr, want) {
			t.Errorf("YAML should contain %q, but got:\n%s", want, yamlStr)
		}
	}
}

// ---------------------------------------------------------------------------
// Mock prompter edge cases
// ---------------------------------------------------------------------------

func TestMockPrompter_UnexpectedCall(t *testing.T) {
	t.Parallel()
	p := &mockPrompter{
		responses: []string{"only-one"},
	}
	_, err := p.Prompt("first")
	if err != nil {
		t.Fatalf("first call should succeed, got: %v", err)
	}
	_, err = p.Prompt("second")
	if err == nil {
		t.Fatal("second call should fail (no more responses)")
	}
	if !strings.Contains(err.Error(), "unexpected prompt call") {
		t.Errorf("error = %q, want \"unexpected prompt call\"", err.Error())
	}
}

func TestMockPrompter_PerCallErrors(t *testing.T) {
	t.Parallel()
	p := &mockPrompter{
		responses: []string{"ok", "err", "ok"},
		errors:    []error{nil, fmt.Errorf("boom"), nil},
	}

	_, err := p.Prompt("q1")
	if err != nil {
		t.Fatalf("q1 should succeed: %v", err)
	}
	resp, err := p.Prompt("q2")
	if err == nil {
		t.Fatal("q2 should return error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("q2 error = %q, want \"boom\"", err.Error())
	}
	if resp != "err" {
		t.Errorf("q2 response = %q, want \"err\"", resp)
	}
	_, err = p.Prompt("q3")
	if err != nil {
		t.Fatalf("q3 should succeed: %v", err)
	}
}
