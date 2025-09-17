package agent

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// TestDebugLog tests the debug logging functionality
func TestDebugLog(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Enable debug mode
	agent.debug = true

	// Test debug log (this shouldn't panic)
	agent.debugLog("Test debug message: %s", "test")

	// Disable debug mode
	agent.debug = false

	// Test debug log when disabled (should be no-op)
	agent.debugLog("This should not be logged")
}

// TestFormatTokenCount tests token count formatting
func TestFormatTokenCount(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{10000, "10.0K"},
		{15432, "15.4K"},
		{999900, "999.9K"},
		{999950, "1000.0K"},
		{1000000, "1.00M"},
		{1420000, "1.42M"},
		{10000000, "10.00M"},
	}

	for _, test := range tests {
		result := agent.formatTokenCount(test.input)
		if result != test.expected {
			t.Errorf("formatTokenCount(%d) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

// TestEstimateContextTokens tests context token estimation
func TestEstimateContextTokens(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	messages := []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello, how are you?"},
		{Role: "assistant", Content: "I'm doing well, thank you!", ReasoningContent: "The user is greeting me."},
	}

	tokens := agent.estimateContextTokens(messages)
	if tokens <= 0 {
		t.Error("Expected positive token count")
	}

	// Test with empty messages
	emptyTokens := agent.estimateContextTokens([]api.Message{})
	if emptyTokens != 0 {
		t.Errorf("Expected 0 tokens for empty messages, got %d", emptyTokens)
	}
}

// TestSuggestCorrectToolName tests tool name suggestion functionality
func TestSuggestCorrectToolName(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"exec", "shell_command"},
		{"bash", "shell_command"},
		{"cmd", "shell_command"},
		{"read", "read_file"},
		{"cat", "read_file"},
		{"write", "write_file"},
		{"edit", "edit_file"},
		{"todo", "add_todo"},
		{"unknown", ""},
	}

	for _, test := range tests {
		result := agent.suggestCorrectToolName(test.input)
		if result != test.expected {
			t.Errorf("suggestCorrectToolName(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

// TestGetProviderEnvVar tests environment variable mapping
func TestGetProviderEnvVar(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	tests := []struct {
		provider api.ClientType
		expected string
	}{
		{api.DeepInfraClientType, "DEEPINFRA_API_KEY"},
		{api.OpenRouterClientType, "OPENROUTER_API_KEY"},
		{api.DeepSeekClientType, "DEEPSEEK_API_KEY"},
		{api.OllamaClientType, ""},
		{"unknown", ""},
	}

	for _, test := range tests {
		result := agent.getProviderEnvVar(test.provider)
		if result != test.expected {
			t.Errorf("getProviderEnvVar(%q) = %q, expected %q", test.provider, result, test.expected)
		}
	}
}

// TestGetModelContextLimit tests context limit retrieval
func TestGetModelContextLimit(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	limit := agent.getModelContextLimit()
	if limit <= 0 {
		t.Error("Expected positive context limit")
	}

	// Should at least be the default fallback value
	if limit < 32000 {
		t.Errorf("Expected context limit to be at least 32000, got %d", limit)
	}
}
