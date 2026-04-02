package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
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
		content := `{"steps":[{"prompt":"hi","max_iterations":-1}]}`
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

	t.Run("orchestration defaults", func(t *testing.T) {
		path := filepath.Join(dir, "orchestration-defaults.json")
		content := `{
			"orchestration": {"enabled": true},
			"steps":[{"prompt":"hi"}]
		}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := loadAgentWorkflowConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Orchestration == nil || !cfg.Orchestration.Enabled {
			t.Fatalf("expected orchestration enabled")
		}
		if cfg.Orchestration.StateFile != defaultWorkflowOrchestrationStateFile {
			t.Fatalf("unexpected default state file: %q", cfg.Orchestration.StateFile)
		}
		if cfg.Orchestration.EventsFile != defaultWorkflowOrchestrationEventsFile {
			t.Fatalf("unexpected default events file: %q", cfg.Orchestration.EventsFile)
		}
		if cfg.Orchestration.ConversationSessionID != defaultWorkflowConversationSessionID {
			t.Fatalf("unexpected default conversation session id: %q", cfg.Orchestration.ConversationSessionID)
		}
		if !cfg.orchestrationResumeEnabled() {
			t.Fatalf("expected resume default enabled")
		}
		if !cfg.orchestrationYieldOnProviderHandoff() {
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

	state := &workflowExecutionState{
		Version:          1,
		InitialCompleted: true,
		NextStepIndex:    2,
		HasError:         true,
		FirstError:       "boom",
		LastProvider:     "ai-worker",
	}
	if err := persistWorkflowExecutionState(cfg, state); err != nil {
		t.Fatalf("persist state: %v", err)
	}

	loaded, err := loadWorkflowExecutionState(cfg)
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
		state *workflowExecutionState
		want  bool
	}{
		{
			name:  "nil state",
			state: nil,
			want:  false,
		},
		{
			name:  "fresh state does not restore",
			state: &workflowExecutionState{Version: 1},
			want:  false,
		},
		{
			name:  "initial completed restores",
			state: &workflowExecutionState{Version: 1, InitialCompleted: true},
			want:  true,
		},
		{
			name:  "advanced step restores",
			state: &workflowExecutionState{Version: 1, NextStepIndex: 2},
			want:  true,
		},
		{
			name:  "error state restores",
			state: &workflowExecutionState{Version: 1, HasError: true},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRestoreWorkflowConversationState(tc.state)
			if got != tc.want {
				t.Fatalf("shouldRestoreWorkflowConversationState()=%t want=%t", got, tc.want)
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
		if err := runtime.validate("test"); err == nil {
			t.Fatalf("expected validation error for empty persona key")
		}
	})

	t.Run("whitespace-only persona key errors", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"   ": {Provider: "deepinfra"},
			},
		}
		if err := runtime.validate("test"); err == nil {
			t.Fatalf("expected validation error for whitespace-only persona key")
		}
	})

	t.Run("missing both provider and model errors", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {},
			},
		}
		if err := runtime.validate("test"); err == nil {
			t.Fatalf("expected validation error when both provider and model are empty")
		}
	})

	t.Run("provider only is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {Provider: "deepinfra"},
			},
		}
		if err := runtime.validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("model only is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"coder": {Model: "claude-haiku"},
			},
		}
		if err := runtime.validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("both provider and model is valid", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"tester": {Provider: "anthropic", Model: "claude-4-haiku"},
			},
		}
		if err := runtime.validate("test"); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("normalizes persona keys", func(t *testing.T) {
		runtime := AgentWorkflowRuntime{
			SubagentOverrides: WorkflowSubagentOverrides{
				"Code-Reviewer": {Provider: "openrouter"},
			},
		}
		if err := runtime.validate("test"); err != nil {
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

		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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

		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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

		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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

		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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

		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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
		applyWorkflowSubagentOverrides(subagentTypes, overrides)

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

	key, found := findSubagentTypeMapKey(subagentTypes, "tester")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester, got key=%q found=%t", key, found)
	}

	key, found = findSubagentTypeMapKey(subagentTypes, "tester")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester by normalized key, got key=%q found=%t", key, found)
	}

	key, found = findSubagentTypeMapKey(subagentTypes, "code_reviewer")
	if !found || key != "code_reviewer" {
		t.Fatalf("expected to find code_reviewer by normalized key, got key=%q found=%t", key, found)
	}

	key, found = findSubagentTypeMapKey(subagentTypes, "qa_checker")
	if !found || key != "tester" {
		t.Fatalf("expected to find tester via normalized alias, got key=%q found=%t", key, found)
	}

	key, found = findSubagentTypeMapKey(subagentTypes, "nonexistent")
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

	cfg, err := loadAgentWorkflowConfig(path)
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
