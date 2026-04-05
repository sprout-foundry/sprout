package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// TryDirectExecution (agent_query.go)
// =============================================================================

func TestTryDirectExecution_EmptyQuery(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for empty query")
	}
}

func TestTryDirectExecution_WhitespaceOnly(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for whitespace-only query")
	}
}

func TestTryDirectExecution_ExactMatches(t *testing.T) {
	commands := []string{
		"pwd", "ls", "ll", "la", "date", "whoami", "id",
		"uname", "hostname", "uptime",
		"git status", "git st", "git log", "git branch",
		"git diff", "git remote", "git stash", "git tag",
		"free", "df", "du", "ps", "env",
	}
	for _, cmd := range commands {
		t.Run("exact_"+cmd, func(t *testing.T) {
			executed, err := TryDirectExecution(context.Background(), nil, cmd)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", cmd, err)
			}
			if !executed {
				t.Errorf("expected true (executed) for %q", cmd)
			}
		})
	}
}

func TestTryDirectExecution_ExactMatchCaseSensitivity(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "PWD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for uppercase 'PWD'")
	}

	executed, err = TryDirectExecution(context.Background(), nil, "Ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for mixed-case 'Ls'")
	}
}

func TestTryDirectExecution_PrefixMatches_WhichAndWhereis(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"which ls", "which ls"},
		{"which go", "which go"},
		{"whereis bash", "whereis bash"},
		{"whereis python3", "whereis python3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed, err := TryDirectExecution(context.Background(), nil, tt.query)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.query, err)
			}
			if !executed {
				t.Errorf("expected true (executed) for %q", tt.query)
			}
		})
	}
}

func TestTryDirectExecution_BareWhichAndWhereis(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "which")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for bare 'which' (no argument)")
	}
}

func TestTryDirectExecution_NaturalLanguageMatches(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"current directory", "current directory"},
		{"current dir", "current dir"},
		{"working directory", "working directory"},
		{"what's the date", "what's the date"},
		{"what time", "what time"},
		{"who am i", "who am i"},
		{"what user", "what user"},
		{"disk space", "disk space"},
		{"disk usage", "disk usage"},
		{"memory", "memory"},
		{"ram", "ram"},
		{"show me the files", "show me the files"},
		{"list files", "list files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed, err := TryDirectExecution(context.Background(), nil, tt.query)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.query, err)
			}
			if !executed {
				t.Errorf("expected true (executed) for natural language %q", tt.query)
			}
		})
	}
}

func TestTryDirectExecution_NaturalLanguageCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"CURRENT DIRECTORY", "CURRENT DIRECTORY"},
		{"Current Dir", "Current Dir"},
		{"What User", "What User"},
		{"DISK SPACE", "DISK SPACE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed, err := TryDirectExecution(context.Background(), nil, tt.query)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.query, err)
			}
			if !executed {
				t.Errorf("expected true (executed) for %q", tt.query)
			}
		})
	}
}

func TestTryDirectExecution_NaturalLanguageTooLong(t *testing.T) {
	// 77 chars — well above the 60-char threshold in TryDirectExecution
	longQuery := "what is the current directory that we are working in right now please tell me thanks"
	executed, err := TryDirectExecution(context.Background(), nil, longQuery)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for query >= 60 chars even with matching pattern")
	}
}

func TestTryDirectExecution_NonMatchingQuery(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "explain quantum physics to me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed {
		t.Error("expected false for non-matching query")
	}
}

func TestTryDirectExecution_GitLogNaturalLanguage(t *testing.T) {
	executed, err := TryDirectExecution(context.Background(), nil, "show the git log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected true for 'show the git log' (natural language contains 'git log')")
	}
}

// =============================================================================
// normalizeReasoningEffort (agent_workflow.go)
// =============================================================================

func TestNormalizeReasoningEffort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace", "  ", ""},
		{"low", "low", "low"},
		{"medium", "medium", "medium"},
		{"high", "high", "high"},
		{"uppercase LOW", "LOW", "low"},
		{"uppercase MEDIUM", "MEDIUM", "medium"},
		{"uppercase HIGH", "HIGH", "high"},
		{"mixed Medium", "Medium", "medium"},
		{"mixed HiGh", "HiGh", "high"},
		{"padded low", "  low  ", "low"},
		{"invalid", "invalid", ""},
		{"turbo", "turbo", ""},
		{"number", "1", ""},
		{"partial lowes", "lowes", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeReasoningEffort(tt.input)
			if got != tt.want {
				t.Errorf("normalizeReasoningEffort(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// normalizeWorkflowWhen, isValidWorkflowWhen, normalizeWorkflowPaths, normalizeWorkflowPersonaID
// =============================================================================

func TestNormalizeWorkflowWhen(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "always"},
		{"always", "always"},
		{"ALWAYS", "always"},
		{"on_success", "on_success"},
		{"ON_SUCCESS", "on_success"},
		{"  on_error  ", "on_error"},
		{"on_error", "on_error"},
		{"invalid", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeWorkflowWhen(tt.input)
			if got != tt.want {
				t.Errorf("normalizeWorkflowWhen(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidWorkflowWhen(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"always", true},
		{"on_success", true},
		{"on_error", true},
		{"ALWAYS", false},
		{"", false},
		{"invalid", false},
		{"on_success_something", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isValidWorkflowWhen(tt.input); got != tt.want {
				t.Errorf("isValidWorkflowWhen(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeWorkflowPaths(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"normal", []string{"a.txt", "b.md"}, []string{"a.txt", "b.md"}},
		{"whitespace", []string{"  a.txt  ", "  ", "b.md"}, []string{"a.txt", "b.md"}},
		{"all whitespace", []string{"  ", "\t"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeWorkflowPaths(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeWorkflowPersonaID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"test", "test"},
		{"Test-Persona", "test_persona"},
		{"MY-PERSONA", "my_persona"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeWorkflowPersonaID(tt.input); got != tt.want {
				t.Errorf("normalizeWorkflowPersonaID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// stepFileTriggersSatisfied (agent_workflow.go)
// =============================================================================

func TestStepFileTriggersSatisfied_NoConditions(t *testing.T) {
	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{})
	if err != nil || !satisfied {
		t.Fatalf("expected (true, nil), got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_FileExists_TempFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(f, []byte("data"), 0644)

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{FileExists: []string{f}})
	if err != nil || !satisfied {
		t.Fatalf("expected (true, nil) for existing file, got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_FileExists_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "missing.txt")

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{FileExists: []string{f}})
	if err != nil || satisfied {
		t.Fatalf("expected (false, nil) for missing file, got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_FileNotExists_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "absent.txt")

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{FileNotExists: []string{f}})
	if err != nil || !satisfied {
		t.Fatalf("expected (true, nil) when file absent, got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_FileNotExists_Existing(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "present.txt")
	os.WriteFile(f, []byte("data"), 0644)

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{FileNotExists: []string{f}})
	if err != nil || satisfied {
		t.Fatalf("expected (false, nil) when FileNotExists file exists, got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_BothMet(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "e.txt")
	missing := filepath.Join(tmpDir, "m.txt")
	os.WriteFile(existing, []byte("x"), 0644)

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{
		FileExists:    []string{existing},
		FileNotExists: []string{missing},
	})
	if err != nil || !satisfied {
		t.Fatalf("expected (true, nil), got (%v, %v)", satisfied, err)
	}
}

func TestStepFileTriggersSatisfied_MultipleFileExists_OneMissing(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "e.txt")
	missing := filepath.Join(tmpDir, "m.txt")
	os.WriteFile(existing, []byte("x"), 0644)

	satisfied, err := stepFileTriggersSatisfied(AgentWorkflowStep{
		FileExists: []string{existing, missing},
	})
	if err != nil || satisfied {
		t.Fatalf("expected (false, nil) when one FileExists fails, got (%v, %v)", satisfied, err)
	}
}

// =============================================================================
// resolveWorkflowTextOrFile (agent_workflow.go)
// =============================================================================

func TestResolveWorkflowTextOrFile_TextOnly(t *testing.T) {
	result, err := resolveWorkflowTextOrFile("my prompt", "", "prompt")
	if err != nil || result != "my prompt" {
		t.Fatalf("unexpected result (%q, %v)", result, err)
	}
}

func TestResolveWorkflowTextOrFile_FileOnly(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "prompt.txt")
	os.WriteFile(f, []byte("file content here"), 0644)

	result, err := resolveWorkflowTextOrFile("", f, "prompt")
	if err != nil || result != "file content here" {
		t.Fatalf("unexpected result (%q, %v)", result, err)
	}
}

func TestResolveWorkflowTextOrFile_BothSet(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "prompt.txt")
	os.WriteFile(f, []byte("content"), 0644)

	_, err := resolveWorkflowTextOrFile("text prompt", f, "prompt")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestResolveWorkflowTextOrFile_NeitherSet(t *testing.T) {
	result, err := resolveWorkflowTextOrFile("", "", "prompt")
	if err != nil || result != "" {
		t.Fatalf("expected empty, got (%q, %v)", result, err)
	}
}

func TestResolveWorkflowTextOrFile_FileNotFound(t *testing.T) {
	_, err := resolveWorkflowTextOrFile("", "/nonexistent/path.txt", "prompt")
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestResolveWorkflowTextOrFile_CustomLabel(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "system.txt")
	os.WriteFile(f, []byte("system prompt content"), 0644)

	result, err := resolveWorkflowTextOrFile("", f, "system_prompt")
	if err != nil || result != "system prompt content" {
		t.Fatalf("unexpected result (%q, %v)", result, err)
	}
}

func TestResolveWorkflowTextOrFile_WhitespaceTrimmed(t *testing.T) {
	result, err := resolveWorkflowTextOrFile("  spaced content  ", "", "label")
	if err != nil || result != "spaced content" {
		t.Fatalf("unexpected result (%q, %v)", result, err)
	}
}

// =============================================================================
// resolveWorkflowInitialPrompt (agent_workflow.go)
// =============================================================================

func TestResolveWorkflowInitialPrompt_CLIQuery(t *testing.T) {
	result, err := resolveWorkflowInitialPrompt("my CLI query", nil)
	if err != nil || result != "my CLI query" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_NoCLINoConfig(t *testing.T) {
	result, err := resolveWorkflowInitialPrompt("", nil)
	if err != nil || result != "" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_NoCLINilInitial(t *testing.T) {
	cfg := &AgentWorkflowConfig{Initial: nil}
	result, err := resolveWorkflowInitialPrompt("", cfg)
	if err != nil || result != "" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_NoCLIConfigHasPrompt(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{Prompt: "config prompt"},
	}
	result, err := resolveWorkflowInitialPrompt("", cfg)
	if err != nil || result != "config prompt" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_NoCLIConfigHasPromptFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "prompt.txt")
	os.WriteFile(f, []byte("file prompt content"), 0644)

	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{PromptFile: f},
	}
	result, err := resolveWorkflowInitialPrompt("", cfg)
	if err != nil || result != "file prompt content" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_CLIQueryTakesPriority(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "prompt.txt")
	os.WriteFile(f, []byte("file prompt content"), 0644)

	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{PromptFile: f},
	}
	result, err := resolveWorkflowInitialPrompt("CLI override", cfg)
	if err != nil || result != "CLI override" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

func TestResolveWorkflowInitialPrompt_WhitespaceCLIFallsThrough(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{Prompt: "config prompt"},
	}
	result, err := resolveWorkflowInitialPrompt("  ", cfg)
	if err != nil || result != "config prompt" {
		t.Fatalf("unexpected (%q, %v)", result, err)
	}
}

// =============================================================================
// shouldRunWorkflowStep (agent_workflow.go)
// =============================================================================

// Note: TestShouldRunWorkflowStep already defined in agent_workflow_test.go.
// This test extends coverage for the empty-string-defaults-to-always path (with "" when).
func TestShouldRunWorkflowStep_EmptyWhenVariants(t *testing.T) {
	tests := []struct {
		name     string
		when     string
		hasError bool
		want     bool
	}{
		{"empty with error", "", true, true},
		{"empty no error", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRunWorkflowStep(tt.when, tt.hasError); got != tt.want {
				t.Errorf("shouldRunWorkflowStep(%q, %v) = %v, want %v", tt.when, tt.hasError, got, tt.want)
			}
		})
	}
}

// =============================================================================
// loadAgentWorkflowConfig (agent_workflow.go)
// =============================================================================

func TestLoadAgentWorkflowConfig_EmptyPath(t *testing.T) {
	cfg, err := loadAgentWorkflowConfig("")
	if err != nil || cfg != nil {
		t.Fatalf("expected (nil, nil), got (%+v, %v)", cfg, err)
	}
}

func TestLoadAgentWorkflowConfig_WhitespacePath(t *testing.T) {
	cfg, err := loadAgentWorkflowConfig("   ")
	if err != nil || cfg != nil {
		t.Fatalf("expected (nil, nil), got (%+v, %v)", cfg, err)
	}
}

func TestLoadAgentWorkflowConfig_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "workflow.json")
	config := AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Name: "step1", Prompt: "do something"}},
	}
	data, _ := json.Marshal(config)
	os.WriteFile(f, data, 0644)

	cfg, err := loadAgentWorkflowConfig(f)
	if err != nil || cfg == nil || len(cfg.Steps) != 1 || cfg.Steps[0].Name != "step1" {
		t.Fatalf("unexpected (%+v, %v)", cfg, err)
	}
}

func TestLoadAgentWorkflowConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "bad.json")
	os.WriteFile(f, []byte("not valid json{{{"), 0644)

	_, err := loadAgentWorkflowConfig(f)
	if err == nil || !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadAgentWorkflowConfig_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "nonexistent.json")
	_, err := loadAgentWorkflowConfig(f)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestLoadAgentWorkflowConfig_InitialOnly(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "workflow.json")
	os.WriteFile(f, []byte(`{"initial":{"prompt":"initial prompt"},"steps":[]}`), 0644)

	cfg, err := loadAgentWorkflowConfig(f)
	if err != nil || cfg == nil || cfg.Initial.Prompt != "initial prompt" {
		t.Fatalf("unexpected (%+v, %v)", cfg, err)
	}
}

func TestLoadAgentWorkflowConfig_NoStepsNoInitial(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "workflow.json")
	os.WriteFile(f, []byte(`{"steps":[]}`), 0644)
	_, err := loadAgentWorkflowConfig(f)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// =============================================================================
// AgentWorkflowConfig.validate() (agent_workflow.go)
// =============================================================================

func TestValidate_NilConfig(t *testing.T) {
	var cfg *AgentWorkflowConfig
	if err := cfg.validate(); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestValidate_NegativeWebPort(t *testing.T) {
	p := -1
	cfg := &AgentWorkflowConfig{Steps: []AgentWorkflowStep{{Prompt: "t"}}, WebPort: &p}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "web_port must be >= 0") {
		t.Fatalf("expected web_port error, got: %v", err)
	}
}

func TestValidate_ZeroWebPort(t *testing.T) {
	p := 0
	cfg := &AgentWorkflowConfig{Steps: []AgentWorkflowStep{{Prompt: "t"}}, WebPort: &p}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_StepBothPromptAndFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "text", PromptFile: "file.txt"}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual exclusive error, got: %v", err)
	}
}

func TestValidate_StepMissingPromptAndFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Name: "empty"}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "requires prompt or prompt_file") {
		t.Fatalf("expected missing prompt error, got: %v", err)
	}
}

func TestValidate_StepInvalidWhen(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "p", When: "invalid_when"}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "when must be one of") {
		t.Fatalf("expected when error, got: %v", err)
	}
}

func TestValidate_InitialBothPromptAndFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "step"}},
		Initial: &AgentWorkflowInitial{Prompt: "ip", PromptFile: "if"},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual exclusive error for initial, got: %v", err)
	}
}

func TestValidate_RuntimeInvalidReasoningEffort(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{ReasoningEffort: "turbo"},
		}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "reasoning_effort must be one of") {
		t.Fatalf("expected reasoning_effort error, got: %v", err)
	}
}

func TestValidate_RuntimeBothSystemPromptAndFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{
				SystemPrompt: "sys", SystemPromptFile: "sys.txt",
			},
		}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "system_prompt_file are mutually exclusive") {
		t.Fatalf("expected system_prompt mutual exclusive error, got: %v", err)
	}
}

func TestValidate_RuntimeNegativeMaxIterations(t *testing.T) {
	n := -1
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{MaxIterations: &n},
		}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "max_iterations must be >= 0") {
		t.Fatalf("expected max_iterations error, got: %v", err)
	}
}

func TestValidate_RuntimeSubagentOverrideEmptyPersona(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{
				SubagentOverrides: WorkflowSubagentOverrides{
					"": workflowSubagentOverride{Provider: "p"},
				},
			},
		}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "empty persona key") {
		t.Fatalf("expected empty persona key error, got: %v", err)
	}
}

func TestValidate_RuntimeSubagentOverrideMissingProviderAndModel(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{
				SubagentOverrides: WorkflowSubagentOverrides{
					"test": workflowSubagentOverride{},
				},
			},
		}},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "must have at least one of provider or model") {
		t.Fatalf("expected subagent override error, got: %v", err)
	}
}

func TestValidate_RuntimeValidSubagentOverride(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{
				SubagentOverrides: WorkflowSubagentOverrides{
					"test-persona": workflowSubagentOverride{Provider: "openai"},
				},
			},
		}},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RuntimeValidMaxIterationsZero(t *testing.T) {
	n := 0
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{
			Prompt: "t",
			AgentWorkflowRuntime: AgentWorkflowRuntime{MaxIterations: &n},
		}},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_OrchestrationDefaults(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify defaults were filled in
	if cfg.Orchestration.StateFile == "" {
		t.Error("expected default state_file")
	}
	if cfg.Orchestration.EventsFile == "" {
		t.Error("expected default events_file")
	}
	if cfg.Orchestration.ConversationSessionID == "" {
		t.Error("expected default conversation_session_id")
	}
}

func TestValidate_ContinueOnError(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps:           []AgentWorkflowStep{{Prompt: "t"}},
		ContinueOnError: true,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// AgentWorkflowConfig orchestration helpers (agent_workflow.go)
// =============================================================================

func TestOrchestrationEnabled_NilConfig(t *testing.T) {
	var cfg *AgentWorkflowConfig
	if cfg.orchestrationEnabled() {
		t.Error("expected false for nil config")
	}
}

func TestOrchestrationEnabled_NilOrchestration(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: nil}
	if cfg.orchestrationEnabled() {
		t.Error("expected false for nil orchestration")
	}
}

func TestOrchestrationEnabled_Disabled(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: false}}
	if cfg.orchestrationEnabled() {
		t.Error("expected false for disabled")
	}
}

func TestOrchestrationEnabled_Enabled(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true}}
	if !cfg.orchestrationEnabled() {
		t.Error("expected true for enabled")
	}
}

// =============================================================================
// shouldPersistRuntimeOverrides (agent_workflow.go)
// =============================================================================

func TestShouldPersistRuntimeOverrides_NilConfig(t *testing.T) {
	var cfg *AgentWorkflowConfig
	if !cfg.shouldPersistRuntimeOverrides() {
		t.Error("expected true (default) for nil config")
	}
}

func TestShouldPersistRuntimeOverrides_NilPersist(t *testing.T) {
	cfg := &AgentWorkflowConfig{PersistRuntimeOverrides: nil}
	if !cfg.shouldPersistRuntimeOverrides() {
		t.Error("expected true when PersistRuntimeOverrides is nil")
	}
}

func TestShouldPersistRuntimeOverrides_True(t *testing.T) {
	v := true
	cfg := &AgentWorkflowConfig{PersistRuntimeOverrides: &v}
	if !cfg.shouldPersistRuntimeOverrides() {
		t.Error("expected true")
	}
}

func TestShouldPersistRuntimeOverrides_False(t *testing.T) {
	v := false
	cfg := &AgentWorkflowConfig{PersistRuntimeOverrides: &v}
	if cfg.shouldPersistRuntimeOverrides() {
		t.Error("expected false when explicitly set to false")
	}
}

// =============================================================================
// orchestrationResumeEnabled / orchestrationYieldOnProviderHandoff
// =============================================================================

func TestOrchestrationResumeEnabled_NilOrchestration(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true, Resume: nil}}
	if !cfg.orchestrationResumeEnabled() {
		t.Error("expected true when Resume is nil (default)")
	}
}

func TestOrchestrationResumeEnabled_False(t *testing.T) {
	f := false
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true, Resume: &f}}
	if cfg.orchestrationResumeEnabled() {
		t.Error("expected false when Resume is false")
	}
}

func TestOrchestrationYieldOnProviderHandoff_Nil(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true, YieldOnProviderHandoff: nil}}
	if !cfg.orchestrationYieldOnProviderHandoff() {
		t.Error("expected true when YieldOnProviderHandoff is nil (default)")
	}
}

func TestOrchestrationYieldOnProviderHandoff_False(t *testing.T) {
	f := false
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true, YieldOnProviderHandoff: &f}}
	if cfg.orchestrationYieldOnProviderHandoff() {
		t.Error("expected false when YieldOnProviderHandoff is false")
	}
}

// =============================================================================
// newWorkflowExecutionState (agent_workflow.go)
// =============================================================================

func TestNewWorkflowExecutionState(t *testing.T) {
	state := newWorkflowExecutionState()
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.Version != 1 {
		t.Errorf("expected version 1, got %d", state.Version)
	}
	if state.NextStepIndex != 0 {
		t.Errorf("expected NextStepIndex 0, got %d", state.NextStepIndex)
	}
}

// =============================================================================
// loadWorkflowExecutionState (agent_workflow.go)
// =============================================================================

func TestLoadWorkflowExecutionState_NotEnabled(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: false}}
	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil || state.Version != 1 {
		t.Fatalf("expected new state, got %+v", state)
	}
}

func TestLoadWorkflowExecutionState_ResumeDisabled(t *testing.T) {
	f := false
	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: true, Resume: &f},
	}
	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil || state.Version != 1 {
		t.Fatalf("expected new state, got %+v", state)
	}
}

func TestLoadWorkflowExecutionState_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "nonexistent.json")
	ef := filepath.Join(tmpDir, "events.jsonl")

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    ef,
		},
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil || state.Version != 1 {
		t.Fatalf("expected new state, got %+v", state)
	}
}

func TestLoadWorkflowExecutionState_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	ef := filepath.Join(tmpDir, "events.jsonl")
	os.WriteFile(sf, []byte(`{
		"version": 1,
		"initial_completed": true,
		"next_step_index": 2,
		"has_error": false
	}`), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    ef,
		},
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if !state.InitialCompleted {
		t.Error("expected InitialCompleted=true")
	}
	if state.NextStepIndex != 2 {
		t.Errorf("expected NextStepIndex 2, got %d", state.NextStepIndex)
	}
}

func TestLoadWorkflowExecutionState_VersionZeroGetsBumped(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte(`{
		"version": 0,
		"next_step_index": 3
	}`), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Version != 1 {
		t.Errorf("expected version bumped to 1, got %d", state.Version)
	}
}

func TestLoadWorkflowExecutionState_NegativeNextStepIndexGetsCorrected(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte(`{
		"version": 1,
		"next_step_index": -5
	}`), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.NextStepIndex != 0 {
		t.Errorf("expected NextStepIndex corrected to 0, got %d", state.NextStepIndex)
	}
}

func TestLoadWorkflowExecutionState_CompletedReturnsNew(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte(`{
		"version": 1,
		"complete": true,
		"next_step_index": 99
	}`), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Completed state should return a new (reset) state
	if state.Complete {
		t.Error("expected new state, not the completed one")
	}
	if state.NextStepIndex != 0 {
		t.Errorf("expected new state with NextStepIndex 0, got %d", state.NextStepIndex)
	}
}

func TestLoadWorkflowExecutionState_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte("not json{{{"), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	_, err := loadWorkflowExecutionState(cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON state")
	}
}

// =============================================================================
// persistWorkflowExecutionState (agent_workflow.go)
// =============================================================================

func TestPersistWorkflowExecutionState_NilState(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			StateFile: filepath.Join(tmpDir, "state.json"),
		},
	}
	cfg.validate()

	if err := persistWorkflowExecutionState(cfg, nil); err != nil {
		t.Fatalf("unexpected error for nil state: %v", err)
	}
}

func TestPersistWorkflowExecutionState_NotEnabled(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: false}}
	state := newWorkflowExecutionState()
	if err := persistWorkflowExecutionState(cfg, state); err != nil {
		t.Fatalf("unexpected error when not enabled: %v", err)
	}
}

func TestPersistWorkflowExecutionState_EmptyStateFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			StateFile: "",
		},
	}
	state := newWorkflowExecutionState()
	err := persistWorkflowExecutionState(cfg, state)
	if err == nil {
		t.Fatal("expected error for empty state file path")
	}
}

func TestPersistWorkflowExecutionState_PersistAndVerify(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "subdir", "state.json")

	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			StateFile: sf,
		},
	}
	cfg.validate()

	state := newWorkflowExecutionState()
	state.InitialCompleted = true
	state.NextStepIndex = 3

	if err := persistWorkflowExecutionState(cfg, state); err != nil {
		t.Fatalf("persist error: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(sf)
	if err != nil {
		t.Fatalf("failed to read persisted state: %v", err)
	}
	if !strings.Contains(string(data), `"initial_completed": true`) {
		t.Errorf("expected initial_completed in persisted state, got: %s", string(data))
	}
	if !strings.Contains(string(data), `"next_step_index": 3`) {
		t.Errorf("expected next_step_index=3 in persisted state, got: %s", string(data))
	}
	if !strings.Contains(string(data), `"updated_at"`) {
		t.Errorf("expected updated_at in persisted state, got: %s", string(data))
	}
}

func TestPersistWorkflowExecutionState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	ef := filepath.Join(tmpDir, "events.jsonl")

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:       true,
			StateFile:     sf,
			EventsFile:    ef,
		},
	}
	cfg.validate()

	original := newWorkflowExecutionState()
	original.InitialCompleted = true
	original.NextStepIndex = 5
	original.HasError = true
	original.FirstError = "something went wrong"

	if err := persistWorkflowExecutionState(cfg, original); err != nil {
		t.Fatalf("persist error: %v", err)
	}

	loaded, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if loaded.InitialCompleted != original.InitialCompleted {
		t.Errorf("InitialCompleted mismatch: got %v, want %v", loaded.InitialCompleted, original.InitialCompleted)
	}
	if loaded.NextStepIndex != original.NextStepIndex {
		t.Errorf("NextStepIndex mismatch: got %d, want %d", loaded.NextStepIndex, original.NextStepIndex)
	}
	if loaded.HasError != original.HasError {
		t.Errorf("HasError mismatch: got %v, want %v", loaded.HasError, original.HasError)
	}
	if loaded.FirstError != original.FirstError {
		t.Errorf("FirstError mismatch: got %q, want %q", loaded.FirstError, original.FirstError)
	}
}

// =============================================================================
// emitWorkflowOrchestrationEvent (agent_workflow.go)
// =============================================================================

func TestEmitWorkflowOrchestrationEvent_NotEnabled(t *testing.T) {
	cfg := &AgentWorkflowConfig{Orchestration: &AgentWorkflowOrchestrationConfig{Enabled: false}}
	if err := emitWorkflowOrchestrationEvent(cfg, "test", nil); err != nil {
		t.Fatalf("unexpected error when not enabled: %v", err)
	}
}

func TestEmitWorkflowOrchestrationEvent_NilConfig(t *testing.T) {
	var cfg *AgentWorkflowConfig
	if err := emitWorkflowOrchestrationEvent(cfg, "test", nil); err != nil {
		t.Fatalf("unexpected error for nil config: %v", err)
	}
}

func TestEmitWorkflowOrchestrationEvent_ValidEvent(t *testing.T) {
	tmpDir := t.TempDir()
	ef := filepath.Join(tmpDir, "events.jsonl")

	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			EventsFile: ef,
		},
	}
	cfg.validate()

	payload := map[string]interface{}{"step_index": 1, "step_name": "test-step"}
	if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_started", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(ef)
	if err != nil {
		t.Fatalf("failed to read events file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "workflow_step_started") {
		t.Errorf("expected event type in events file, got: %s", content)
	}
	if !strings.Contains(content, "step_name") {
		t.Errorf("expected payload in events file, got: %s", content)
	}
	if !strings.Contains(content, "timestamp") {
		t.Errorf("expected timestamp in events file, got: %s", content)
	}
}

func TestEmitWorkflowOrchestrationEvent_EmptyEventsFile(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			EventsFile: "",
		},
	}
	err := emitWorkflowOrchestrationEvent(cfg, "test", nil)
	if err == nil {
		t.Fatal("expected error for empty events file path")
	}
}

func TestEmitWorkflowOrchestrationEvent_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	ef := filepath.Join(tmpDir, "multi_events.jsonl")

	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:   true,
			EventsFile: ef,
		},
	}
	cfg.validate()

	events := []map[string]interface{}{
		{"action": "start"},
		{"action": "progress"},
		{"action": "complete"},
	}
	for _, ev := range events {
		if err := emitWorkflowOrchestrationEvent(cfg, "test_event", ev); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	data, _ := os.ReadFile(ef)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 event lines, got %d", len(lines))
	}
	// Verify each line is valid JSON
	for i, line := range lines {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// =============================================================================
// displayVerboseLog 20000-line truncation (log.go)
// =============================================================================

func TestDisplayVerboseLog_Truncation(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ledit"), 0755)

	// Write 25001 lines to just exceed the 20000 line limit.
	// We use short lines and only slightly above the limit to avoid
	// pipe buffer deadlocks in captureStdout.
	var buf strings.Builder
	for i := 0; i < 25001; i++ {
		buf.WriteString("x\n")
	}
	logFile := filepath.Join(dir, ".ledit", "workspace.log")
	os.WriteFile(logFile, []byte(buf.String()), 0644)

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	out := captureStdout(t, displayVerboseLog)

	if !strings.Contains(out, "Displaying last 20000 lines") {
		t.Errorf("expected truncation message, got output length %d", len(out))
	}
	if !strings.Contains(out, "total 25001 lines available") {
		t.Errorf("expected total line count in output, got: %s", out)
	}
}

// =============================================================================
// applyWorkflowCommandOverrides (agent_workflow.go)
// =============================================================================

func TestApplyWorkflowCommandOverrides_NilConfig(t *testing.T) {
	// Should not panic
	applyWorkflowCommandOverrides(nil)
}

func TestApplyWorkflowCommandOverrides_NilFlags(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
	}
	// All flags are nil, should not panic
	applyWorkflowCommandOverrides(cfg)
}

func TestApplyWorkflowCommandOverrides_NoWebUI(t *testing.T) {
	orig := disableWebUI
	defer func() { disableWebUI = orig }()

	f := true
	cfg := &AgentWorkflowConfig{NoWebUI: &f}
	applyWorkflowCommandOverrides(cfg)
	if !disableWebUI {
		t.Error("expected disableWebUI to be set to true")
	}
}

func TestApplyWorkflowCommandOverrides_WebPort(t *testing.T) {
	orig := webPort
	defer func() { webPort = orig }()

	p := 9999
	cfg := &AgentWorkflowConfig{WebPort: &p}
	applyWorkflowCommandOverrides(cfg)
	if webPort != 9999 {
		t.Errorf("expected webPort=9999, got %d", webPort)
	}
}

func TestApplyWorkflowCommandOverrides_DaemonMode(t *testing.T) {
	orig := daemonMode
	defer func() { daemonMode = orig }()

	f := true
	cfg := &AgentWorkflowConfig{Daemon: &f}
	applyWorkflowCommandOverrides(cfg)
	if !daemonMode {
		t.Error("expected daemonMode to be set to true")
	}
}

// =============================================================================
// shouldRestoreWorkflowConversationState (agent_workflow.go)
// =============================================================================

func TestShouldRestoreWorkflowConversationState_Nil(t *testing.T) {
	if shouldRestoreWorkflowConversationState(nil) {
		t.Error("expected false for nil state")
	}
}

func TestShouldRestoreWorkflowConversationState_FreshState(t *testing.T) {
	state := newWorkflowExecutionState()
	if shouldRestoreWorkflowConversationState(state) {
		t.Error("expected false for fresh state")
	}
}

func TestShouldRestoreWorkflowConversationState_InitialCompleted(t *testing.T) {
	state := newWorkflowExecutionState()
	state.InitialCompleted = true
	if !shouldRestoreWorkflowConversationState(state) {
		t.Error("expected true when InitialCompleted=true")
	}
}

func TestShouldRestoreWorkflowConversationState_NextStepPositive(t *testing.T) {
	state := newWorkflowExecutionState()
	state.NextStepIndex = 2
	if !shouldRestoreWorkflowConversationState(state) {
		t.Error("expected true when NextStepIndex > 0")
	}
}

func TestShouldRestoreWorkflowConversationState_HasError(t *testing.T) {
	state := newWorkflowExecutionState()
	state.HasError = true
	if !shouldRestoreWorkflowConversationState(state) {
		t.Error("expected true when HasError=true")
	}
}

func TestShouldRestoreWorkflowConversationState_FirstErrorSet(t *testing.T) {
	state := newWorkflowExecutionState()
	state.FirstError = "oops"
	if !shouldRestoreWorkflowConversationState(state) {
		t.Error("expected true when FirstError is set")
	}
}