package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDelegateConfigValidate(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:        "write a unit test",
			MaxIterations: 50,
		}
		err := cfg.Validate()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg.MaxIterations != 50 {
			t.Errorf("expected MaxIterations to remain 50, got %d", cfg.MaxIterations)
		}
	})

	t.Run("EmptyPrompt", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:        "",
			MaxIterations: 10,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for empty prompt, got nil")
		}
		if !strings.Contains(err.Error(), "prompt is required") {
			t.Errorf("expected error containing 'prompt is required', got %q", err.Error())
		}
	})

	t.Run("DefaultsMaxIterations", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:        "review this code",
			MaxIterations: 0,
		}
		err := cfg.Validate()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg.MaxIterations != DefaultMaxDelegateIterations {
			t.Errorf("expected MaxIterations to default to %d, got %d", DefaultMaxDelegateIterations, cfg.MaxIterations)
		}
	})

	t.Run("NegativeMaxIterations", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:        "debug this issue",
			MaxIterations: -1,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for negative MaxIterations, got nil")
		}
		if !strings.Contains(err.Error(), "max_iterations must be non-negative") {
			t.Errorf("expected error containing 'max_iterations must be non-negative', got %q", err.Error())
		}
	})
}

func TestDelegateExitStatusConstants(t *testing.T) {
	if DelegateExitCompleted != "completed" {
		t.Errorf("expected DelegateExitCompleted to be 'completed', got %q", DelegateExitCompleted)
	}
	if DelegateExitMaxIterations != "max_iterations" {
		t.Errorf("expected DelegateExitMaxIterations to be 'max_iterations', got %q", DelegateExitMaxIterations)
	}
	if DelegateExitError != "error" {
		t.Errorf("expected DelegateExitError to be 'error', got %q", DelegateExitError)
	}
}

func TestDelegateConstants(t *testing.T) {
	if DefaultMaxDelegateIterations != 100 {
		t.Errorf("expected DefaultMaxDelegateIterations to be 100, got %d", DefaultMaxDelegateIterations)
	}
	if MaxDelegateNestingDepth != 3 {
		t.Errorf("expected MaxDelegateNestingDepth to be 3, got %d", MaxDelegateNestingDepth)
	}
}

func TestDelegateResultJSON(t *testing.T) {
	result := DelegateResult{
		Summary:      "Reviewed 3 files",
		FilesChanged: []string{"pkg/agent/test.go"},
		ToolsCalled: []ToolCallRecord{
			{ToolName: "read_file", Input: "pkg/agent/test.go", Output: "...", Timestamp: time.Now(), Duration: 42, Success: true},
		},
		TokensUsed: 1500,
		Cost:       0.05,
		Iterations: 5,
		ExitStatus: DelegateExitCompleted,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded DelegateResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Summary != result.Summary {
		t.Errorf("Summary mismatch: %q vs %q", decoded.Summary, result.Summary)
	}
	if decoded.ErrorMessage != "" {
		t.Errorf("omitempty should have suppressed empty error_message, got %q", decoded.ErrorMessage)
	}
}
