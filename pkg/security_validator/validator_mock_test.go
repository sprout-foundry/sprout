package security_validator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// MockLLM is a mock implementation of the LLM model for testing
type MockLLM struct {
	Response string
	Error    error
}

func (m *MockLLM) Completion(ctx context.Context, prompt string, opts ...interface{}) (string, error) {
	if m.Error != nil {
		return "", m.Error
	}
	return m.Response, nil
}

// TestValidateToolCallWithMock tests validation with a mock LLM
func TestValidateToolCallWithMock(t *testing.T) {
	tests := []struct {
		name             string
		mockResponse     string
		mockError        error
		expectedRisk     RiskLevel
		expectedReason   string
		expectedConfirm  bool
		threshold        int
	}{
		{
			name:          "Safe operation",
			mockResponse:  `{"risk_level": 0, "reasoning": "Safe to execute", "confidence": 0.95}`,
			expectedRisk:  RiskSafe,
			expectedReason: "Safe to execute",
			expectedConfirm: false,
			threshold:     1,
		},
		{
			name:          "Cautious operation with threshold 1",
			mockResponse:  `{"risk_level": 1, "reasoning": "Be careful", "confidence": 0.8}`,
			expectedRisk:  RiskCaution,
			expectedReason: "Be careful",
			expectedConfirm: true,
			threshold:     1,
		},
		{
			name:          "Dangerous operation with threshold 1",
			mockResponse:  `{"risk_level": 2, "reasoning": "Very dangerous", "confidence": 0.9}`,
			expectedRisk:  RiskDangerous,
			expectedReason: "Very dangerous",
			expectedConfirm: true,
			threshold:     1,
		},
		{
			name:          "Dangerous operation with threshold 2",
			mockResponse:  `{"risk_level": 2, "reasoning": "Very dangerous", "confidence": 0.9}`,
			expectedRisk:  RiskDangerous,
			expectedReason: "Very dangerous",
			expectedConfirm: true,
			threshold:     2,
		},
		{
			name:          "Caution operation with threshold 2",
			mockResponse:  `{"risk_level": 1, "reasoning": "Be careful", "confidence": 0.8}`,
			expectedRisk:  RiskCaution,
			expectedReason: "Be careful",
			expectedConfirm: false,
			threshold:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				config: &configuration.SecurityValidationConfig{
					Enabled:   true,
					Threshold: tt.threshold,
				},
				model:       &MockLLM{Response: tt.mockResponse},
				modelPath:   "/test/model.gguf",
				logger:      utils.GetLogger(true),
				interactive: false,
			}

			ctx := context.Background()
			result, err := validator.ValidateToolCall(ctx, "shell_command", map[string]interface{}{
				"command": "test command",
			})

			if err != nil {
				t.Fatalf("ValidateToolCall failed: %v", err)
			}

			if result.RiskLevel != tt.expectedRisk {
				t.Errorf("Expected risk level %v, got %v", tt.expectedRisk, result.RiskLevel)
			}

			if result.Reasoning != tt.expectedReason {
				t.Errorf("Expected reasoning '%s', got '%s'", tt.expectedReason, result.Reasoning)
			}

			if result.ShouldConfirm != tt.expectedConfirm {
				t.Errorf("Expected ShouldConfirm=%v, got %v", tt.expectedConfirm, result.ShouldConfirm)
			}

			if result.ModelUsed != "/test/model.gguf" {
				t.Errorf("Expected ModelUsed='/test/model.gguf', got '%s'", result.ModelUsed)
			}

			if result.LatencyMs < 0 {
				t.Error("Expected positive latency")
			}
		})
	}
}

// TestValidateToolCallLLMError tests error handling when LLM fails
func TestValidateToolCallLLMError(t *testing.T) {
	validator := &Validator{
		config: &configuration.SecurityValidationConfig{
			Enabled:   true,
			Threshold: 1,
		},
		model:       &MockLLM{Error: context.DeadlineExceeded},
		modelPath:   "/test/model.gguf",
		logger:      utils.GetLogger(true),
		interactive: false,
	}

	ctx := context.Background()
	result, err := validator.ValidateToolCall(ctx, "shell_command", map[string]interface{}{
		"command": "test",
	})

	if err != nil {
		t.Fatalf("ValidateToolCall should not fail on LLM error: %v", err)
	}

	if result.RiskLevel != RiskCaution {
		t.Errorf("Expected RiskCaution on LLM error, got %v", result.RiskLevel)
	}

	if !contains(result.Reasoning, "failed") {
		t.Errorf("Expected error message in reasoning, got '%s'", result.Reasoning)
	}

	if result.ShouldConfirm {
		t.Error("Should not request confirmation when LLM fails")
	}
}

// TestBuildValidationPrompt tests various tool calls
func TestBuildValidationPromptVariousTools(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{},
		modelPath: "/test/model.gguf",
	}

	tests := []struct {
		name        string
		toolName    string
		args        map[string]interface{}
		shouldMatch []string
	}{
		{
			name:     "Read file",
			toolName: "read_file",
			args:     map[string]interface{}{"file_path": "main.go"},
			shouldMatch: []string{"read_file", "main.go"},
		},
		{
			name:     "Write file",
			toolName: "write_file",
			args:     map[string]interface{}{"file_path": "test.txt", "content": "hello"},
			shouldMatch: []string{"write_file", "test.txt", "hello"},
		},
		{
			name:     "Shell command",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "rm -rf /tmp/test"},
			shouldMatch: []string{"shell_command", "rm -rf /tmp/test"},
		},
		{
			name:     "Search files",
			toolName: "search_files",
			args:     map[string]interface{}{"search_pattern": "TODO", "directory": "."},
			shouldMatch: []string{"search_files", "TODO", "."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := validator.buildValidationPrompt(tt.toolName, tt.args)

			for _, match := range tt.shouldMatch {
				if !contains(prompt, match) {
					t.Errorf("Prompt should contain '%s'", match)
				}
			}
		})
	}
}

// TestValidationResultCompleteFlow tests end-to-end validation flow
func TestValidationResultCompleteFlow(t *testing.T) {
	// Test complete flow from validation to result
	mockResponse := `{"risk_level": 2, "reasoning": "This is dangerous", "confidence": 0.95}`

	validator := &Validator{
		config: &configuration.SecurityValidationConfig{
			Enabled:   true,
			Threshold: 1,
		},
		model:       &MockLLM{Response: mockResponse},
		modelPath:   "/test/model.gguf",
		logger:      utils.GetLogger(true),
		interactive: false, // Disable interactive to test just the validation logic
	}

	ctx := context.Background()
	result, err := validator.ValidateToolCall(ctx, "shell_command", map[string]interface{}{
		"command": "rm -rf /important/data",
	})

	if err != nil {
		t.Fatalf("ValidateToolCall failed: %v", err)
	}

	// Verify result structure
	if result.RiskLevel != RiskDangerous {
		t.Errorf("Expected RiskDangerous, got %v", result.RiskLevel)
	}

	if result.Reasoning != "This is dangerous" {
		t.Errorf("Expected 'This is dangerous', got '%s'", result.Reasoning)
	}

	if result.Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", result.Confidence)
	}

	if result.ModelUsed != "/test/model.gguf" {
		t.Errorf("Expected model path '/test/model.gguf', got '%s'", result.ModelUsed)
	}

	if result.Timestamp <= 0 {
		t.Error("Expected positive timestamp")
	}

	if result.LatencyMs < 0 {
		t.Error("Expected non-negative latency")
	}

	// With threshold 1 and risk 2, should confirm
	if !result.ShouldConfirm {
		t.Error("Expected ShouldConfirm=true with threshold 1 and risk 2")
	}

	if result.ShouldBlock {
		t.Error("Expected ShouldBlock=false before user decision")
	}
}

// TestApplyThresholdEdgeCases tests threshold edge cases
func TestApplyThresholdEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		threshold     int
		riskLevel     RiskLevel
		shouldConfirm bool
	}{
		{"Zero threshold with risk 0", 0, RiskSafe, false},
		{"Zero threshold with risk 1", 0, RiskCaution, true},
		{"Zero threshold with risk 2", 0, RiskDangerous, true},
		{"High threshold with low risk", 3, RiskSafe, false},
		{"Negative threshold defaults to 1", -1, RiskCaution, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				config: &configuration.SecurityValidationConfig{
					Threshold: tt.threshold,
				},
			}

			result := &ValidationResult{
				RiskLevel: tt.riskLevel,
			}

			result = validator.applyThreshold(result)

			if result.ShouldConfirm != tt.shouldConfirm {
				t.Errorf("Expected ShouldConfirm=%v, got %v", tt.shouldConfirm, result.ShouldConfirm)
			}

			if result.ShouldBlock {
				t.Error("ShouldBlock should always be false after applyThreshold")
			}
		})
	}
}

// TestParseValidationResponseWithMarkdown tests JSON parsing with markdown
func TestParseValidationResponseWithMarkdown(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "JSON in markdown code block",
			response: "```json\n{\"risk_level\": 1, \"reasoning\": \"Test\", \"confidence\": 0.8}\n```",
		},
		{
			name:     "JSON without language identifier",
			response: "```\n{\"risk_level\": 1, \"reasoning\": \"Test\", \"confidence\": 0.8}\n```",
		},
		{
			name:     "Plain JSON",
			response: `{"risk_level": 1, "reasoning": "Test", "confidence": 0.8}`,
		},
		{
			name:     "JSON with text before",
			response: "Here's my analysis:\n```json\n{\"risk_level\": 1, \"reasoning\": \"Test\", \"confidence\": 0.8}\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := time.Now()
			result, err := validator.parseValidationResponse(tt.response, startTime)

			if err != nil {
				t.Fatalf("parseValidationResponse failed: %v", err)
			}

			if result.RiskLevel != RiskCaution {
				t.Errorf("Expected RiskCaution, got %v", result.RiskLevel)
			}

			if result.Reasoning != "Test" {
				t.Errorf("Expected 'Test', got '%s'", result.Reasoning)
			}
		})
	}
}

// TestValidationResultSerializationRoundTrip tests JSON round-trip
func TestValidationResultSerializationRoundTrip(t *testing.T) {
	original := ValidationResult{
		RiskLevel:     RiskDangerous,
		Reasoning:     "Test with special chars: <>&\"'",
		Confidence:    0.87,
		Timestamp:     1234567890,
		ModelUsed:     "/path/to/model.gguf",
		LatencyMs:     42,
		ShouldBlock:   false,
		ShouldConfirm: true,
	}

	// Marshal to JSON
	bytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal back
	var unmarshaled ValidationResult
	err = json.Unmarshal(bytes, &unmarshaled)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify all fields match
	if unmarshaled.RiskLevel != original.RiskLevel {
		t.Errorf("RiskLevel mismatch")
	}

	if unmarshaled.Reasoning != original.Reasoning {
		t.Errorf("Reasoning mismatch: got '%s', want '%s'", unmarshaled.Reasoning, original.Reasoning)
	}

	if unmarshaled.Confidence != original.Confidence {
		t.Errorf("Confidence mismatch")
	}

	if unmarshaled.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch")
	}

	if unmarshaled.ModelUsed != original.ModelUsed {
		t.Errorf("ModelUsed mismatch")
	}

	if unmarshaled.LatencyMs != original.LatencyMs {
		t.Errorf("LatencyMs mismatch")
	}

	if unmarshaled.ShouldBlock != original.ShouldBlock {
		t.Errorf("ShouldBlock mismatch")
	}

	if unmarshaled.ShouldConfirm != original.ShouldConfirm {
		t.Errorf("ShouldConfirm mismatch")
	}
}

// TestIsCriticalSystemOperation tests the critical system operation detection
func TestIsCriticalSystemOperation(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		expected bool
	}{
		// Always blocked - truly destructive operations
		{
			name:     "mkfs command - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "mkfs /dev/sda1"},
			expected: true,
		},
		{
			name:     "rm -rf / - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "rm -rf /"},
			expected: true,
		},
		{
			name:     "rm -rf . - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "rm -rf ."},
			expected: true,
		},
		{
			name:     "fork bomb - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": ":(){:|:&};:"},
			expected: true,
		},
		{
			name:     "killall -9 - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "killall -9 python"},
			expected: true,
		},
		{
			name:     "chmod 000 / - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "chmod 000 /"},
			expected: true,
		},
		{
			name:     "fdisk on primary disk - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "fdisk /dev/sda"},
			expected: true,
		},
		{
			name:     "dd zero to primary disk - critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "dd if=/dev/zero of=/dev/sda"},
			expected: true,
		},
		{
			name:     "write to /etc/shadow - critical",
			toolName: "write_file",
			args:     map[string]interface{}{"file_path": "/etc/shadow", "content": "malicious"},
			expected: true,
		},
		{
			name:     "write to /etc/passwd - critical",
			toolName: "write_file",
			args:     map[string]interface{}{"file_path": "/etc/passwd", "content": "malicious"},
			expected: true,
		},
		{
			name:     "edit /etc/sudoers - critical",
			toolName: "edit_file",
			args:     map[string]interface{}{"file_path": "/etc/sudoers", "content": "malicious"},
			expected: true,
		},
		// Allowed operations - have legitimate use cases
		{
			name:     "normal shell command - not critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "ls -la"},
			expected: false,
		},
		{
			name:     "write to normal file - not critical",
			toolName: "write_file",
			args:     map[string]interface{}{"file_path": "/tmp/test.txt", "content": "test"},
			expected: false,
		},
		{
			name:     "read file - not critical",
			toolName: "read_file",
			args:     map[string]interface{}{"file_path": "/etc/passwd"},
			expected: false,
		},
		{
			name:     "fdisk on secondary disk - not critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "fdisk /dev/sdb"},
			expected: false,
		},
		{
			name:     "parted on secondary disk - not critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "parted /dev/sdc"},
			expected: false,
		},
		{
			name:     "dd to secondary disk - not critical (legitimate use)",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "dd if=bootable.img of=/dev/sdb"},
			expected: false,
		},
		{
			name:     "dd from secondary disk - not critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "dd if=/dev/sdb of=backup.img"},
			expected: false,
		},
		{
			name:     "dd zero to secondary disk - not critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "dd if=/dev/zero of=/dev/sdb"},
			expected: false,
		},
		{
			name:     "mkfs on secondary disk - still critical",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "mkfs /dev/sdb1"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCriticalSystemOperation(tt.toolName, tt.args)
			if result != tt.expected {
				t.Errorf("IsCriticalSystemOperation() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
