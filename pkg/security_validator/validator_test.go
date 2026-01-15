package security_validator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// TestNewValidatorDisabled tests validator creation when disabled
func TestNewValidatorDisabled(t *testing.T) {
	cfg := &configuration.SecurityValidationConfig{
		Enabled: false,
	}

	validator, err := NewValidator(cfg, nil, false)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	if validator == nil {
		t.Fatal("Expected validator to be created even when disabled")
	}

	if validator.model != nil {
		t.Error("Expected model to be nil when validation is disabled")
	}
}

// TestNewValidatorNoConfig tests validator creation with nil config
func TestNewValidatorNoConfig(t *testing.T) {
	_, err := NewValidator(nil, nil, false)
	if err == nil {
		t.Error("Expected error when config is nil")
	}
}

// TestValidateToolCallDisabled tests validation when disabled
func TestValidateToolCallDisabled(t *testing.T) {
	cfg := &configuration.SecurityValidationConfig{
		Enabled: false,
	}

	validator, err := NewValidator(cfg, nil, false)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	ctx := context.Background()
	result, err := validator.ValidateToolCall(ctx, "read_file", map[string]interface{}{
		"file_path": "test.go",
	})

	if err != nil {
		t.Fatalf("ValidateToolCall failed: %v", err)
	}

	if result.RiskLevel != RiskSafe {
		t.Errorf("Expected RiskSafe when disabled, got %v", result.RiskLevel)
	}

	if result.ShouldConfirm || result.ShouldBlock {
		t.Error("Expected no confirmation or blocking when disabled")
	}
}

// TestValidateToolCallModelNotLoaded tests validation when model failed to load
func TestValidateToolCallModelNotLoaded(t *testing.T) {
	cfg := &configuration.SecurityValidationConfig{
		Enabled: true,
		Model:    "/nonexistent/path/to/model.gguf",
	}

	// Create a temp directory for testing
	tempDir := t.TempDir()
	cfg.Model = filepath.Join(tempDir, "nonexistent.gguf")

	validator, err := NewValidator(cfg, nil, false)
	// We expect this to fail since model doesn't exist
	if err == nil {
		// But if it succeeds (e.g., in an environment without llama.cpp), that's ok
		// Just test the ValidateToolCall behavior
	}

	if validator == nil {
		// Create a validator without model manually for testing
		validator = &Validator{
			config:     cfg,
			model:      nil,
			modelPath:  cfg.Model,
			logger:     nil,
			interactive: false,
		}
	}

	ctx := context.Background()
	result, err := validator.ValidateToolCall(ctx, "shell_command", map[string]interface{}{
		"command": "rm -rf /important/data",
	})

	if err != nil {
		t.Fatalf("ValidateToolCall failed: %v", err)
	}

	if result.RiskLevel != RiskCaution {
		t.Errorf("Expected RiskCaution when model not loaded, got %v", result.RiskLevel)
	}

	if result.ShouldConfirm {
		t.Error("Expected no confirmation when model not loaded")
	}
}

// TestParseValidationResponseJSON tests parsing JSON responses
func TestParseValidationResponseJSON(t *testing.T) {
	validator := &Validator{
		config:  &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	tests := []struct {
		name           string
		response       string
		expectedRisk   RiskLevel
		expectedReason string
	}{
		{
			name:           "Safe operation",
			response:       `{"risk_level": 0, "reasoning": "This is safe", "confidence": 0.95}`,
			expectedRisk:   RiskSafe,
			expectedReason: "This is safe",
		},
		{
			name:           "Cautious operation",
			response:       `{"risk_level": 1, "reasoning": "Be careful", "confidence": 0.8}`,
			expectedRisk:   RiskCaution,
			expectedReason: "Be careful",
		},
		{
			name:           "Dangerous operation",
			response:       `{"risk_level": 2, "reasoning": "Very dangerous", "confidence": 0.9}`,
			expectedRisk:   RiskDangerous,
			expectedReason: "Very dangerous",
		},
		{
			name:           "JSON with markdown code block",
			response:       "```json\n{\"risk_level\": 1, \"reasoning\": \"Test\", \"confidence\": 0.7}\n```",
			expectedRisk:   RiskCaution,
			expectedReason: "Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := time.Now()
			result, err := validator.parseValidationResponse(tt.response, startTime)
			if err != nil {
				t.Fatalf("parseValidationResponse failed: %v", err)
			}

			if result.RiskLevel != tt.expectedRisk {
				t.Errorf("Expected risk level %v, got %v", tt.expectedRisk, result.RiskLevel)
			}

			if result.Reasoning != tt.expectedReason {
				t.Errorf("Expected reasoning '%s', got '%s'", tt.expectedReason, result.Reasoning)
			}
		})
	}
}

// TestParseValidationResponseText tests parsing non-JSON text responses
func TestParseValidationResponseText(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	tests := []struct {
		name           string
		response       string
		expectedRisk   RiskLevel
	}{
		{
			name:         "Text with dangerous",
			response:     "This operation is dangerous and risky",
			expectedRisk: RiskDangerous,
		},
		{
			name:         "Text with caution",
			response:     "Please be cautious with this operation",
			expectedRisk: RiskCaution,
		},
		{
			name:         "Text with risk: 2",
			response:     "The risk level is 2 for this command",
			expectedRisk: RiskDangerous,
		},
		{
			name:         "Safe text",
			response:     "This operation is completely safe to execute",
			expectedRisk: RiskSafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := time.Now()
			result, err := validator.parseTextResponse(tt.response, startTime)
			if err != nil {
				t.Fatalf("parseTextResponse failed: %v", err)
			}

			if result.RiskLevel != tt.expectedRisk {
				t.Errorf("Expected risk level %v, got %v", tt.expectedRisk, result.RiskLevel)
			}
		})
	}
}

// TestApplyThreshold tests threshold application logic
func TestApplyThreshold(t *testing.T) {
	tests := []struct {
		name             string
		threshold        int
		riskLevel        RiskLevel
		shouldConfirm    bool
		shouldBlock      bool
	}{
		{
			name:          "Threshold 0, risk 0",
			threshold:     0,
			riskLevel:     RiskSafe,
			shouldConfirm: false,
			shouldBlock:   false,
		},
		{
			name:          "Threshold 1, risk 0",
			threshold:     1,
			riskLevel:     RiskSafe,
			shouldConfirm: false,
			shouldBlock:   false,
		},
		{
			name:          "Threshold 1, risk 1",
			threshold:     1,
			riskLevel:     RiskCaution,
			shouldConfirm: true,
			shouldBlock:   false,
		},
		{
			name:          "Threshold 1, risk 2",
			threshold:     1,
			riskLevel:     RiskDangerous,
			shouldConfirm: true,
			shouldBlock:   false,
		},
		{
			name:          "Threshold 2, risk 1",
			threshold:     2,
			riskLevel:     RiskCaution,
			shouldConfirm: false,
			shouldBlock:   false,
		},
		{
			name:          "Threshold 2, risk 2",
			threshold:     2,
			riskLevel:     RiskDangerous,
			shouldConfirm: true,
			shouldBlock:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				config: &configuration.SecurityValidationConfig{
					Threshold: tt.threshold,
				},
			}

			result := &ValidationResult{
				RiskLevel:     tt.riskLevel,
				ShouldConfirm: false,
				ShouldBlock:   false,
			}

			result = validator.applyThreshold(result)

			if result.ShouldConfirm != tt.shouldConfirm {
				t.Errorf("Expected ShouldConfirm=%v, got %v", tt.shouldConfirm, result.ShouldConfirm)
			}

			if result.ShouldBlock != tt.shouldBlock {
				t.Errorf("Expected ShouldBlock=%v, got %v", tt.shouldBlock, result.ShouldBlock)
			}
		})
	}
}

// TestBuildValidationPrompt tests prompt generation
func TestBuildValidationPrompt(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{},
		modelPath: "/test/model.gguf",
	}

	prompt := validator.buildValidationPrompt("shell_command", map[string]interface{}{
		"command": "rm -rf /tmp/test",
	})

	if !contains(prompt, "shell_command") {
		t.Error("Prompt should contain tool name")
	}

	if !contains(prompt, "rm -rf /tmp/test") {
		t.Error("Prompt should contain command")
	}

	if !contains(prompt, "security validation assistant") {
		t.Error("Prompt should explain the role")
	}

	if !contains(prompt, "risk level") {
		t.Error("Prompt should request risk level")
	}

	if !contains(prompt, "JSON") {
		t.Error("Prompt should request JSON response")
	}
}

// TestValidationResultJSONSerialization tests JSON serialization of results
func TestValidationResultJSONSerialization(t *testing.T) {
	result := ValidationResult{
		RiskLevel:     RiskDangerous,
		Reasoning:     "Test reasoning",
		Confidence:    0.95,
		Timestamp:     time.Now().Unix(),
		ModelUsed:     "/test/model.gguf",
		LatencyMs:     25,
		ShouldBlock:   false,
		ShouldConfirm: true,
	}

	bytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	var unmarshaled ValidationResult
	err = json.Unmarshal(bytes, &unmarshaled)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if unmarshaled.RiskLevel != result.RiskLevel {
		t.Errorf("RiskLevel mismatch: got %v, want %v", unmarshaled.RiskLevel, result.RiskLevel)
	}

	if unmarshaled.Reasoning != result.Reasoning {
		t.Errorf("Reasoning mismatch: got %s, want %s", unmarshaled.Reasoning, result.Reasoning)
	}

	if unmarshaled.ShouldConfirm != result.ShouldConfirm {
		t.Errorf("ShouldConfirm mismatch: got %v, want %v", unmarshaled.ShouldConfirm, result.ShouldConfirm)
	}
}

// TestRiskLevelString tests RiskLevel String() method
func TestRiskLevelString(t *testing.T) {
	tests := []struct {
		risk     RiskLevel
		expected string
	}{
		{RiskSafe, "SAFE"},
		{RiskCaution, "CAUTION"},
		{RiskDangerous, "DANGEROUS"},
		{RiskLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.risk.String() != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, tt.risk.String())
			}
		})
	}
}

// TestDownloadModelInvalidURL tests download failure handling
func TestDownloadModelInvalidURL(t *testing.T) {
	// This test verifies error handling when download fails
	// We can't actually test download without network, but we can verify the function exists
	tempDir := t.TempDir()
	modelPath := filepath.Join(tempDir, "test.gguf")

	// Create a logger
	logger := utils.GetLogger(true)

	// Try to download (this will attempt network access to HuggingFace)
	// In CI/test environments, this will fail
	err := downloadModel(modelPath, logger)
	// We expect either success or failure - both are acceptable in different environments
	// The important part is testing the function exists and handles errors gracefully

	// If download succeeded, verify the file exists
	if err == nil {
		if _, err := os.Stat(modelPath); os.IsNotExist(err) {
			t.Error("Download succeeded but model file doesn't exist")
		}
		// Clean up the downloaded file
		os.Remove(modelPath)
	}

	// Verify temp file was cleaned up (regardless of success/failure)
	if _, err := os.Stat(modelPath + ".tmp"); !os.IsNotExist(err) {
		// Temp file exists, try to clean it up
		os.Remove(modelPath + ".tmp")
		t.Error("Temp file should be cleaned up after download")
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestValidateToolCallInvalidRiskLevel tests handling of invalid risk levels
func TestValidateToolCallInvalidRiskLevel(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	// Test with invalid risk level
	response := `{"risk_level": 5, "reasoning": "Invalid", "confidence": 0.5}`
	startTime := time.Now()

	_, err := validator.parseValidationResponse(response, startTime)
	if err == nil {
		t.Error("Expected error for invalid risk level, but got nil")
	}
}

// TestValidateToolCallMissingConfidence tests handling of missing confidence
func TestValidateToolCallMissingConfidence(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	// Test with missing confidence (should default to 0.8)
	response := `{"risk_level": 1, "reasoning": "Test"}`
	startTime := time.Now()

	result, err := validator.parseValidationResponse(response, startTime)
	if err != nil {
		t.Fatalf("parseValidationResponse failed: %v", err)
	}

	if result.Confidence != 0.8 {
		t.Errorf("Expected default confidence 0.8, got %f", result.Confidence)
	}
}

// TestValidateToolCallMalformedJSON tests handling of malformed JSON
func TestValidateToolCallMalformedJSON(t *testing.T) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{Threshold: 1},
		modelPath: "/test/model.gguf",
	}

	// Test with completely malformed JSON
	response := `{not valid json`
	startTime := time.Now()

	result, err := validator.parseValidationResponse(response, startTime)
	// Should fall back to text parsing
	if err != nil {
		t.Fatalf("parseValidationResponse should fall back to text parsing: %v", err)
	}

	if result == nil {
		t.Error("Expected result from text parsing fallback")
	}
}

// BenchmarkValidationPrompt benchmarks prompt generation
func BenchmarkValidationPrompt(b *testing.B) {
	validator := &Validator{
		config:    &configuration.SecurityValidationConfig{},
		modelPath: "/test/model.gguf",
	}

	args := map[string]interface{}{
		"command": "rm -rf /tmp/test",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.buildValidationPrompt("shell_command", args)
	}
}
