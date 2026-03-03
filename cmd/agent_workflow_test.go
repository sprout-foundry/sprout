package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentWorkflowConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.json")
	content := `{
		"initial": {
			"prompt": "Implement feature X",
			"reasoning_effort": "low",
			"max_iterations": 50
		},
		"persist_runtime_overrides": false,
		"continue_on_error": true,
		"steps": [
			{"name": "audit", "prompt": "Audit the previous response", "when": "on_success", "reasoning_effort": "high"},
			{"prompt": "If there was a failure, explain it", "when": "on_error"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if cfg.Initial == nil {
		t.Fatalf("expected non-nil initial config")
	}
	if cfg.Initial.ReasoningEffort != "low" {
		t.Fatalf("expected initial reasoning_effort low, got %q", cfg.Initial.ReasoningEffort)
	}
	if cfg.Initial.MaxIterations == nil || *cfg.Initial.MaxIterations != 50 {
		t.Fatalf("expected initial max_iterations=50")
	}
	if cfg.shouldPersistRuntimeOverrides() {
		t.Fatalf("expected persist_runtime_overrides=false")
	}
	if len(cfg.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Steps))
	}
	if cfg.Steps[0].When != workflowWhenOnSuccess {
		t.Fatalf("expected first step when=%q, got %q", workflowWhenOnSuccess, cfg.Steps[0].When)
	}
	if cfg.Steps[0].ReasoningEffort != "high" {
		t.Fatalf("expected first step reasoning_effort high, got %q", cfg.Steps[0].ReasoningEffort)
	}
}

func TestLoadAgentWorkflowConfigValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("missing steps", func(t *testing.T) {
		path := filepath.Join(dir, "missing-steps.json")
		if err := os.WriteFile(path, []byte(`{"continue_on_error": true}`), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid when", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-when.json")
		if err := os.WriteFile(path, []byte(`{"steps":[{"prompt":"hello","when":"later"}]}`), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid reasoning_effort", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-reasoning.json")
		if err := os.WriteFile(path, []byte(`{"steps":[{"prompt":"hello","reasoning_effort":"max"}]}`), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("normalizes file triggers", func(t *testing.T) {
		path := filepath.Join(dir, "file-triggers.json")
		content := `{
			"steps":[{"prompt":"hello","file_exists":["  a.txt  ",""],"file_not_exists":["  b.txt "]}]
		}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := loadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if len(cfg.Steps[0].FileExists) != 1 || cfg.Steps[0].FileExists[0] != "a.txt" {
			t.Fatalf("unexpected file_exists normalization: %#v", cfg.Steps[0].FileExists)
		}
		if len(cfg.Steps[0].FileNotExists) != 1 || cfg.Steps[0].FileNotExists[0] != "b.txt" {
			t.Fatalf("unexpected file_not_exists normalization: %#v", cfg.Steps[0].FileNotExists)
		}
	})

	t.Run("prompt and prompt_file are exclusive", func(t *testing.T) {
		path := filepath.Join(dir, "prompt-exclusive.json")
		content := `{"steps":[{"prompt":"hi","prompt_file":"prompt.txt"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("system prompt fields are exclusive", func(t *testing.T) {
		path := filepath.Join(dir, "system-prompt-exclusive.json")
		content := `{"steps":[{"prompt":"hi","system_prompt":"abc","system_prompt_file":"sys.txt"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid max iterations", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-max-iterations.json")
		content := `{"steps":[{"prompt":"hi","max_iterations":0}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid web port", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-web-port.json")
		content := `{"web_port":-1,"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := loadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})
}

func TestShouldRunWorkflowStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		when     string
		hasError bool
		want     bool
	}{
		{name: "always on success", when: workflowWhenAlways, hasError: false, want: true},
		{name: "always on error", when: workflowWhenAlways, hasError: true, want: true},
		{name: "on success", when: workflowWhenOnSuccess, hasError: false, want: true},
		{name: "on success with error", when: workflowWhenOnSuccess, hasError: true, want: false},
		{name: "on error", when: workflowWhenOnError, hasError: true, want: true},
		{name: "on error without error", when: workflowWhenOnError, hasError: false, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunWorkflowStep(tc.when, tc.hasError)
			if got != tc.want {
				t.Fatalf("shouldRunWorkflowStep(%q, %t) = %t, want %t", tc.when, tc.hasError, got, tc.want)
			}
		})
	}
}

func TestResolveWorkflowInitialPrompt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptFile := filepath.Join(dir, "initial_prompt.txt")
	if err := os.WriteFile(promptFile, []byte("Run configured prompt from file"), 0600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{Prompt: "Run configured prompt"},
	}

	if got, err := resolveWorkflowInitialPrompt("from cli", cfg); err != nil || got != "from cli" {
		t.Fatalf("expected CLI prompt to win, got %q err=%v", got, err)
	}
	if got, err := resolveWorkflowInitialPrompt("", cfg); err != nil || got != "Run configured prompt" {
		t.Fatalf("expected configured initial prompt, got %q err=%v", got, err)
	}

	cfg.Initial.Prompt = ""
	cfg.Initial.PromptFile = promptFile
	if got, err := resolveWorkflowInitialPrompt("", cfg); err != nil || got != "Run configured prompt from file" {
		t.Fatalf("expected initial prompt from file, got %q err=%v", got, err)
	}
}

func TestResolveStepPrompt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	promptFile := filepath.Join(dir, "step_prompt.txt")
	if err := os.WriteFile(promptFile, []byte("step prompt from file"), 0600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	prompt, err := resolveStepPrompt(AgentWorkflowStep{Prompt: "inline"})
	if err != nil || prompt != "inline" {
		t.Fatalf("expected inline prompt, got %q err=%v", prompt, err)
	}

	prompt, err = resolveStepPrompt(AgentWorkflowStep{PromptFile: promptFile})
	if err != nil || prompt != "step prompt from file" {
		t.Fatalf("expected file prompt, got %q err=%v", prompt, err)
	}
}

func TestShouldPersistRuntimeOverridesDefault(t *testing.T) {
	t.Parallel()

	cfg := &AgentWorkflowConfig{}
	if !cfg.shouldPersistRuntimeOverrides() {
		t.Fatalf("expected default persist_runtime_overrides=true")
	}
}

func TestStepFileTriggersSatisfied(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	missing := filepath.Join(dir, "missing.txt")
	if err := os.WriteFile(existing, []byte("ok"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	t.Run("exists and not exists pass", func(t *testing.T) {
		ok, err := stepFileTriggersSatisfied(AgentWorkflowStep{
			FileExists:    []string{existing},
			FileNotExists: []string{missing},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected triggers to pass")
		}
	})

	t.Run("exists fails when missing", func(t *testing.T) {
		ok, err := stepFileTriggersSatisfied(AgentWorkflowStep{
			FileExists: []string{missing},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("expected triggers to fail")
		}
	})

	t.Run("not exists fails when present", func(t *testing.T) {
		ok, err := stepFileTriggersSatisfied(AgentWorkflowStep{
			FileNotExists: []string{existing},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("expected triggers to fail")
		}
	})
}
