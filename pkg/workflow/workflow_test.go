//go:build !js

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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

	cfg, err := LoadAgentWorkflowConfig(path)
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
	if cfg.ShouldPersistRuntimeOverrides() {
		t.Fatalf("expected persist_runtime_overrides=false")
	}
	if len(cfg.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Steps))
	}
	if cfg.Steps[0].When != WorkflowWhenOnSuccess {
		t.Fatalf("expected first step when=%q, got %q", WorkflowWhenOnSuccess, cfg.Steps[0].When)
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
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid when", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-when.json")
		if err := os.WriteFile(path, []byte(`{"steps":[{"prompt":"hello","when":"later"}]}`), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid reasoning_effort", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-reasoning.json")
		if err := os.WriteFile(path, []byte(`{"steps":[{"prompt":"hello","reasoning_effort":"max"}]}`), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
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
		cfg, err := LoadAgentWorkflowConfig(path)
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
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("system prompt fields are exclusive", func(t *testing.T) {
		path := filepath.Join(dir, "system-prompt-exclusive.json")
		content := `{"steps":[{"prompt":"hi","system_prompt":"abc","system_prompt_file":"sys.txt"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid max iterations", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-max-iterations.json")
		content := `{"steps":[{"prompt":"hi","max_iterations":-1}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("invalid web port", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-web-port.json")
		content := `{"web_port":-1,"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("shell step with command is valid", func(t *testing.T) {
		path := filepath.Join(dir, "shell-step.json")
		content := `{"steps":[{"name":"build","command":"make build-all","when":"on_success"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.Steps[0].IsShellStep() {
			t.Fatalf("expected step to be a shell step")
		}
		if cfg.Steps[0].Command != "make build-all" {
			t.Fatalf("unexpected command: %q", cfg.Steps[0].Command)
		}
	})

	t.Run("shell step with command_file is valid", func(t *testing.T) {
		path := filepath.Join(dir, "shell-step-file.json")
		content := `{"steps":[{"name":"deploy","command_file":"scripts/deploy.sh"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.Steps[0].IsShellStep() {
			t.Fatalf("expected step to be a shell step")
		}
	})

	t.Run("step with both prompt and command is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "shell-and-prompt.json")
		content := `{"steps":[{"prompt":"hi","command":"ls"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("step with both command and command_file is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "shell-and-shellfile.json")
		content := `{"steps":[{"command":"ls","command_file":"scripts/a.sh"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("step with no work is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "empty-step.json")
		content := `{"steps":[{"name":"empty","when":"always"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("budget usd negative is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "budget-usd-neg.json")
		content := `{"budget":{"usd":-1},"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error for negative budget.usd")
		}
	})

	t.Run("budget warn_at out of range is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "budget-warn-range.json")
		content := `{"budget":{"usd":10,"warn_at":[0.5,1.5]},"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error for warn_at > 1")
		}
	})

	t.Run("budget on_exceed invalid is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "budget-on-exceed.json")
		content := `{"budget":{"usd":10,"on_exceed":"explode"},"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error for unknown on_exceed value")
		}
	})

	t.Run("budget defaults warn_at and on_exceed", func(t *testing.T) {
		path := filepath.Join(dir, "budget-defaults.json")
		content := `{"budget":{"usd":10},"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.Budget.OnExceed != "truncate" {
			t.Fatalf("on_exceed default = %q, want truncate", cfg.Budget.OnExceed)
		}
		if len(cfg.Budget.WarnAt) != 2 || cfg.Budget.WarnAt[0] != 0.5 || cfg.Budget.WarnAt[1] != 0.8 {
			t.Fatalf("warn_at default = %v, want [0.5, 0.8]", cfg.Budget.WarnAt)
		}
	})

	t.Run("progress heartbeat negative is rejected", func(t *testing.T) {
		path := filepath.Join(dir, "progress-neg.json")
		content := `{"progress":{"heartbeat_seconds":-1},"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if _, err := LoadAgentWorkflowConfig(path); err == nil {
			t.Fatalf("expected validation error for negative heartbeat_seconds")
		}
	})

	t.Run("ParseBudgetWarnList valid", func(t *testing.T) {
		thresholds, err := ParseBudgetWarnList("0.5, 0.8")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(thresholds) != 2 || thresholds[0] != 0.5 || thresholds[1] != 0.8 {
			t.Fatalf("got %v, want [0.5, 0.8]", thresholds)
		}
	})

	t.Run("requires_approval defaults to true when unset", func(t *testing.T) {
		path := filepath.Join(dir, "ra-unset.json")
		content := `{"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !cfg.IsApprovalRequired() {
			t.Fatalf("IsApprovalRequired() should be true when JSON omits the field")
		}
	})

	t.Run("requires_approval explicit true", func(t *testing.T) {
		path := filepath.Join(dir, "ra-true.json")
		content := `{"requires_approval":true,"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !cfg.IsApprovalRequired() {
			t.Fatalf("IsApprovalRequired() should be true for explicit true")
		}
	})

	t.Run("requires_approval explicit false", func(t *testing.T) {
		path := filepath.Join(dir, "ra-false.json")
		content := `{"requires_approval":false,"steps":[{"prompt":"hi"}]}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.IsApprovalRequired() {
			t.Fatalf("IsApprovalRequired() should be false for explicit false")
		}
	})

	t.Run("ParseBudgetWarnList rejects out-of-range", func(t *testing.T) {
		if _, err := ParseBudgetWarnList("0.5,1.5"); err == nil {
			t.Fatalf("expected error for 1.5")
		}
		if _, err := ParseBudgetWarnList("0,0.5"); err == nil {
			t.Fatalf("expected error for 0")
		}
	})

	t.Run("orchestration defaults", func(t *testing.T) {
		path := filepath.Join(dir, "orchestration-defaults.json")
		content := `{
			"orchestration": {"enabled": true},
			"steps":[{"prompt":"hi"}]
		}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Orchestration == nil || !cfg.Orchestration.Enabled {
			t.Fatalf("expected orchestration enabled")
		}
		if cfg.Orchestration.StateFile != DefaultWorkflowOrchestrationStateFile {
			t.Fatalf("unexpected default state file: %q", cfg.Orchestration.StateFile)
		}
		if cfg.Orchestration.EventsFile != DefaultWorkflowOrchestrationEventsFile {
			t.Fatalf("unexpected default events file: %q", cfg.Orchestration.EventsFile)
		}
		if cfg.Orchestration.ConversationSessionID != DefaultWorkflowConversationSessionID {
			t.Fatalf("unexpected default conversation session id: %q", cfg.Orchestration.ConversationSessionID)
		}
		if !cfg.OrchestrationResumeEnabled() {
			t.Fatalf("expected resume default enabled")
		}
		if !cfg.OrchestrationYieldOnProviderHandoff() {
			t.Fatalf("expected yield_on_provider_handoff default enabled")
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
		{name: "always on success", when: WorkflowWhenAlways, hasError: false, want: true},
		{name: "always on error", when: WorkflowWhenAlways, hasError: true, want: true},
		{name: "on success", when: WorkflowWhenOnSuccess, hasError: false, want: true},
		{name: "on success with error", when: WorkflowWhenOnSuccess, hasError: true, want: false},
		{name: "on error", when: WorkflowWhenOnError, hasError: true, want: true},
		{name: "on error without error", when: WorkflowWhenOnError, hasError: false, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldRunWorkflowStep(tc.when, tc.hasError)
			if got != tc.want {
				t.Fatalf("ShouldRunWorkflowStep(%q, %t) = %t, want %t", tc.when, tc.hasError, got, tc.want)
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

	if got, err := ResolveWorkflowInitialPrompt("from cli", cfg); err != nil || got != "from cli" {
		t.Fatalf("expected CLI prompt to win, got %q err=%v", got, err)
	}
	if got, err := ResolveWorkflowInitialPrompt("", cfg); err != nil || got != "Run configured prompt" {
		t.Fatalf("expected configured initial prompt, got %q err=%v", got, err)
	}

	cfg.Initial.Prompt = ""
	cfg.Initial.PromptFile = promptFile
	if got, err := ResolveWorkflowInitialPrompt("", cfg); err != nil || got != "Run configured prompt from file" {
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

	prompt, err := ResolveStepPrompt(AgentWorkflowStep{Prompt: "inline"})
	if err != nil || prompt != "inline" {
		t.Fatalf("expected inline prompt, got %q err=%v", prompt, err)
	}

	prompt, err = ResolveStepPrompt(AgentWorkflowStep{PromptFile: promptFile})
	if err != nil || prompt != "step prompt from file" {
		t.Fatalf("expected file prompt, got %q err=%v", prompt, err)
	}
}

func TestShouldPersistRuntimeOverridesDefault(t *testing.T) {
	t.Parallel()

	cfg := &AgentWorkflowConfig{}
	if !cfg.ShouldPersistRuntimeOverrides() {
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
		ok, err := StepFileTriggersSatisfied(AgentWorkflowStep{
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
		ok, err := StepFileTriggersSatisfied(AgentWorkflowStep{
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
		ok, err := StepFileTriggersSatisfied(AgentWorkflowStep{
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

func TestWorkflowExecutionStateRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := &AgentWorkflowConfig{
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:    true,
			StateFile:  filepath.Join(dir, "state", "workflow_state.json"),
			EventsFile: filepath.Join(dir, "events", "workflow_events.jsonl"),
		},
	}

	state := &WorkflowExecutionState{
		Version:          1,
		InitialCompleted: true,
		NextStepIndex:    2,
		HasError:         true,
		FirstError:       "boom",
		LastProvider:     "ai-worker",
	}
	if err := PersistWorkflowExecutionState(cfg, state); err != nil {
		t.Fatalf("persist state: %v", err)
	}

	loaded, err := LoadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.NextStepIndex != 2 || !loaded.InitialCompleted || loaded.LastProvider != "ai-worker" {
		t.Fatalf("unexpected loaded state: %#v", loaded)
	}
}

func TestShouldRestoreWorkflowConversationState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state *WorkflowExecutionState
		want  bool
	}{
		{
			name:  "nil state",
			state: nil,
			want:  false,
		},
		{
			name:  "fresh state does not restore",
			state: &WorkflowExecutionState{Version: 1},
			want:  false,
		},
		{
			name:  "initial completed restores",
			state: &WorkflowExecutionState{Version: 1, InitialCompleted: true},
			want:  true,
		},
		{
			name:  "advanced step restores",
			state: &WorkflowExecutionState{Version: 1, NextStepIndex: 2},
			want:  true,
		},
		{
			name:  "error state restores",
			state: &WorkflowExecutionState{Version: 1, HasError: true},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldRestoreWorkflowConversationState(tc.state)
			if got != tc.want {
				t.Fatalf("ShouldRestoreWorkflowConversationState()=%t want=%t", got, tc.want)
			}
		})
	}
}

func TestSubagentOverridesValidation(t *testing.T) {
	t.Parallel()

	t.Run("empty persona key errors", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"": {Provider: "deepinfra"},
			},
		}
		if err := runtime.Validate("test"); err == nil {
			t.Fatalf("expected validation error for empty persona key")
		}
	})

	t.Run("whitespace-only persona key errors", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"   ": {Provider: "deepinfra"},
			},
		}
		if err := runtime.Validate("test"); err == nil {
			t.Fatalf("expected validation error for whitespace-only persona key")
		}
	})

	t.Run("missing both provider and model errors", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {},
			},
		}
		if err := runtime.Validate("test"); err == nil {
			t.Fatalf("expected validation error when both provider and model are empty")
		}
	})

	t.Run("provider only is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {Provider: "deepinfra"},
			},
		}
		if err := runtime.Validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("model only is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"coder": {Model: "claude-haiku"},
			},
		}
		if err := runtime.Validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("both provider and model is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {Provider: "anthropic", Model: "claude-4-haiku"},
			},
		}
		if err := runtime.Validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("normalizes persona keys", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"Code-Reviewer": {Provider: "openrouter"},
			},
		}
		if err := runtime.Validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})
}

func TestSubagentOverridesApplyAndRestore(t *testing.T) {
	t.Parallel()

	t.Run("apply patches subagent types", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"tester": {
				ID:       "tester",
				Name:     "Tester",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  true,
			},
			"coder": {
				ID:       "coder",
				Name:     "Coder",
				Provider: "openai",
				Model:    "gpt-5",
				Enabled:  true,
			},
		}

		overrides := WorkflowSubagentOverrides{
			"tester": {Provider: "deepinfra", Model: "deepseek-v3"},
		}

		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		if subagentTypes["tester"].Provider != "deepinfra" {
			t.Fatalf("expected tester provider deepinfra, got %q", subagentTypes["tester"].Provider)
		}
		if subagentTypes["tester"].Model != "deepseek-v3" {
			t.Fatalf("expected tester model deepseek-v3, got %q", subagentTypes["tester"].Model)
		}
		// coder should be untouched
		if subagentTypes["coder"].Provider != "openai" {
			t.Fatalf("expected coder provider to remain openai, got %q", subagentTypes["coder"].Provider)
		}
	})

	t.Run("apply skips unknown persona", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"tester": {
				ID:       "tester",
				Name:     "Tester",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  true,
			},
		}

		overrides := WorkflowSubagentOverrides{
			"nonexistent": {Provider: "deepinfra", Model: "deepseek-v3"},
		}

		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		if subagentTypes["tester"].Provider != "anthropic" {
			t.Fatalf("expected tester provider to remain unchanged, got %q", subagentTypes["tester"].Provider)
		}
	})

	t.Run("apply skips disabled persona", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"tester": {
				ID:       "tester",
				Name:     "Tester",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  false,
			},
		}

		overrides := WorkflowSubagentOverrides{
			"tester": {Provider: "deepinfra", Model: "deepseek-v3"},
		}

		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		if subagentTypes["tester"].Provider != "anthropic" {
			t.Fatalf("expected disabled tester provider to remain unchanged, got %q", subagentTypes["tester"].Provider)
		}
	})

	t.Run("apply normalizes persona keys", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"code_reviewer": {
				ID:       "code_reviewer",
				Name:     "Code Reviewer",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  true,
			},
		}

		overrides := WorkflowSubagentOverrides{
			"Code-Reviewer": {Provider: "openrouter", Model: "gemini-2.5-pro"},
		}

		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		if subagentTypes["code_reviewer"].Provider != "openrouter" {
			t.Fatalf("expected code_reviewer provider openrouter, got %q", subagentTypes["code_reviewer"].Provider)
		}
		if subagentTypes["code_reviewer"].Model != "gemini-2.5-pro" {
			t.Fatalf("expected code_reviewer model gemini-2.5-pro, got %q", subagentTypes["code_reviewer"].Model)
		}
	})

	t.Run("apply with aliases matches correct entry", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"tester": {
				ID:       "tester",
				Name:     "Tester",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  true,
				Aliases:  []string{"qa-checker", "test-writer"},
			},
		}

		overrides := WorkflowSubagentOverrides{
			"qa-checker": {Provider: "deepinfra"},
		}

		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		if subagentTypes["tester"].Provider != "deepinfra" {
			t.Fatalf("expected tester provider deepinfra (matched via alias), got %q", subagentTypes["tester"].Provider)
		}
	})

	t.Run("restore returns original values via backup map", func(t *testing.T) {
		subagentTypes := map[string]configuration.SubagentType{
			"tester": {
				ID:       "tester",
				Name:     "Tester",
				Provider: "anthropic",
				Model:    "claude-sonnet-4",
				Enabled:  true,
			},
			"coder": {
				ID:       "coder",
				Name:     "Coder",
				Provider: "openai",
				Model:    "gpt-5",
				Enabled:  true,
			},
		}

		// Create a backup as prepareWorkflowRuntimeRestorer would
		backup := map[string]struct {
			OriginalProvider string
			OriginalModel    string
			OriginalKey      string
		}{
			"tester": {
				OriginalProvider: subagentTypes["tester"].Provider,
				OriginalModel:    subagentTypes["tester"].Model,
				OriginalKey:      "tester",
			},
		}

		// Apply overrides (变形)
		overrides := WorkflowSubagentOverrides{
			"tester": {Provider: "deepinfra", Model: "deepseek-v3"},
		}
		ApplyWorkflowSubagentOverrides(subagentTypes, overrides)

		// Verify mutation happened
		if subagentTypes["tester"].Provider != "deepinfra" {
			t.Fatalf("expected tester provider deepinfra after apply, got %q", subagentTypes["tester"].Provider)
		}

		// Restore (same logic as restore function)
		for _, b := range backup {
			st := subagentTypes[b.OriginalKey]
			st.Provider = b.OriginalProvider
			st.Model = b.OriginalModel
			subagentTypes[b.OriginalKey] = st
		}

		// Verify restoration
		if subagentTypes["tester"].Provider != "anthropic" {
			t.Fatalf("expected tester provider restored to anthropic, got %q", subagentTypes["tester"].Provider)
		}
		if subagentTypes["tester"].Model != "claude-sonnet-4" {
			t.Fatalf("expected tester model restored to claude-sonnet-4, got %q", subagentTypes["tester"].Model)
		}
		// coder should be unchanged throughout
		if subagentTypes["coder"].Provider != "openai" {
			t.Fatalf("expected coder provider to remain openai, got %q", subagentTypes["coder"].Provider)
		}
	})
}

func TestFindSubagentTypeMapKey(t *testing.T) {
	t.Parallel()

	subagentTypes := map[string]configuration.SubagentType{
		"tester": {
			ID:      "tester",
			Enabled: true,
			Aliases: []string{"qa-checker"},
		},
		"code_reviewer": {
			ID:      "code_reviewer",
			Enabled: true,
		},
	}

	key, found := FindSubagentTypeMapKey(subagentTypes, "tester")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester, got key=%q found=%t", key, found)
	}

	key, found = FindSubagentTypeMapKey(subagentTypes, "tester")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester by normalized key, got key=%q found=%t", key, found)
	}

	key, found = FindSubagentTypeMapKey(subagentTypes, "code_reviewer")
	if !found || key != "code_reviewer" {
		t.Fatalf("expected to find code_reviewer by normalized key, got key=%q found=%t", key, found)
	}

	key, found = FindSubagentTypeMapKey(subagentTypes, "qa_checker")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester via normalized alias, got key=%q found=%t", key, found)
	}

	key, found = FindSubagentTypeMapKey(subagentTypes, "nonexistent")
	if found {
		t.Fatalf("expected not to find nonexistent, got key=%q", key)
	}
}

func TestSubagentOverridesWorkflowConfigParsing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "subagent-workflow.json")
	content := `{
		"initial": {
			"prompt": "Implement feature X",
			"subagent_overrides": {
				"tester": {"provider": "anthropic", "model": "claude-haiku-4"},
				"code-reviewer": {"provider": "openrouter", "model": "google/gemini-2.5-pro"}
			}
		},
		"steps": [
			{
				"name": "cheap_tests",
				"subagent_overrides": {
					"tester": {"provider": "deepinfra", "model": "deepseek-v3"}
				},
				"prompt": "Write all tests"
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}

	// Verify initial subagent overrides
	if len(cfg.Initial.SubagentOverrides) != 2 {
		t.Fatalf("expected 2 initial subagent overrides, got %d", len(cfg.Initial.SubagentOverrides))
	}
	if cfg.Initial.SubagentOverrides["tester"].Provider != "anthropic" {
		t.Fatalf("expected initial tester provider anthropic, got %q", cfg.Initial.SubagentOverrides["tester"].Provider)
	}
	if cfg.Initial.SubagentOverrides["code-reviewer"].Model != "google/gemini-2.5-pro" {
		t.Fatalf("expected initial code-reviewer model google/gemini-2.5-pro, got %q", cfg.Initial.SubagentOverrides["code-reviewer"].Model)
	}

	// Verify step subagent overrides
	if len(cfg.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(cfg.Steps))
	}
	if len(cfg.Steps[0].SubagentOverrides) != 1 {
		t.Fatalf("expected 1 step subagent override, got %d", len(cfg.Steps[0].SubagentOverrides))
	}
	if cfg.Steps[0].SubagentOverrides["tester"].Provider != "deepinfra" {
		t.Fatalf("expected step tester provider deepinfra, got %q", cfg.Steps[0].SubagentOverrides["tester"].Provider)
	}
}

func TestPickSubagentDefault(t *testing.T) {
	t.Parallel()

	t.Run("orchestrator present", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{
			"orchestrator": {Provider: "ai-worker", Model: "qwen3.6-27b"},
			"coder":        {Provider: "openai", Model: "gpt-5"},
		}
		pick := pickSubagentDefault(overrides)
		if pick.Provider != "ai-worker" || pick.Model != "qwen3.6-27b" {
			t.Fatalf("expected orchestrator override, got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("coder present only", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{
			"coder": {Provider: "openai", Model: "gpt-5"},
		}
		pick := pickSubagentDefault(overrides)
		if pick.Provider != "openai" || pick.Model != "gpt-5" {
			t.Fatalf("expected coder override, got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("empty map returns zero value", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{}
		pick := pickSubagentDefault(overrides)
		if pick.Provider != "" || pick.Model != "" {
			t.Fatalf("expected zero value, got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("alphabetical tiebreak", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{
			"tester":        {Provider: "anthropic", Model: "claude-haiku"},
			"code_reviewer": {Provider: "openrouter", Model: "gemini-2.5-pro"},
		}
		pick := pickSubagentDefault(overrides)
		// "code_reviewer" sorts before "tester" alphabetically
		if pick.Provider != "openrouter" || pick.Model != "gemini-2.5-pro" {
			t.Fatalf("expected code_reviewer (alphabetical first), got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("skips entries with both empty provider and model", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{
			"orchestrator": {},
			"coder":        {Provider: "openai", Model: "gpt-5"},
		}
		pick := pickSubagentDefault(overrides)
		if pick.Provider != "openai" || pick.Model != "gpt-5" {
			t.Fatalf("expected coder override, got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("normalizes keys (case-insensitive, hyphens→underscores)", func(t *testing.T) {
		// "Orchestrator" (capitalized) should normalize to "orchestrator"
		// and be preferred over the coder entry.
		overrides := WorkflowSubagentOverrides{
			"Orchestrator": {Provider: "ai-worker", Model: "qwen3.6-27b"},
			"Coder":        {Provider: "openai", Model: "gpt-5"},
		}
		pick := pickSubagentDefault(overrides)
		// "Orchestrator" normalizes to "orchestrator" and is preferred over coder.
		if pick.Provider != "ai-worker" || pick.Model != "qwen3.6-27b" {
			t.Fatalf("expected Orchestrator override (normalized), got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})

	t.Run("coder-only with mixed casing", func(t *testing.T) {
		overrides := WorkflowSubagentOverrides{
			"CODER": {Provider: "openai", Model: "gpt-5"},
		}
		pick := pickSubagentDefault(overrides)
		if pick.Provider != "openai" || pick.Model != "gpt-5" {
			t.Fatalf("expected CODER override (normalized to coder), got provider=%q model=%q", pick.Provider, pick.Model)
		}
	})
}
