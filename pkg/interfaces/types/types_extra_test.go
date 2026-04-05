package types

import (
	"encoding/json"
	"testing"
)

// --- Invalid JSON Tests ---

func TestProviderConfig_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"malformed JSON", `{"name": "test"`, true},
		{"trailing comma", `{"name": "test",}`, true},
		{"invalid type for string", `{"name": 123}`, true},
		{"invalid type for number", `{"temperature": "hot"}`, true},
		{"invalid type for bool", `{"enabled": "yes"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg ProviderConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentConfig_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"invalid type for int", `{"max_retries": "three"}`, true},
		{"invalid type for bool", `{"enable_validation": "true"}`, true},
		{"invalid type for float", `{"cost_threshold": "high"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg AgentConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEditorConfig_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"invalid type for slice", `{"ignore_patterns": "not-an-array"}`, true},
		{"invalid type for int", `{"max_file_size": "large"}`, true},
		{"invalid type for bool", `{"backup_enabled": 1}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg EditorConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityConfig_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"invalid type for slice", `{"blocked_patterns": "pattern1,pattern2"}`, true},
		{"invalid type for bool", `{"require_confirmation": "yes"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg SecurityConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUIConfig_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"invalid type for bool", `{"skip_prompts": "true"}`, true},
		{"invalid type for string", `{"output_format": 123}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg UIConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Unknown Fields Tests ---

func TestProviderConfig_UnknownFieldsIgnored(t *testing.T) {
	jsonData := `{
		"name": "test-provider",
		"model": "test-model",
		"unknown_field": "should-be-ignored",
		"another_unknown": 123,
		"nested_unknown": {"key": "value"}
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Name != "test-provider" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "test-provider")
	}
	if cfg.Model != "test-model" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "test-model")
	}
}

func TestAgentConfig_UnknownFieldsIgnored(t *testing.T) {
	jsonData := `{
		"max_retries": 5,
		"unknown_field": true,
		"another_unknown": "ignored"
	}`

	var cfg AgentConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries: got %d, want 5", cfg.MaxRetries)
	}
}

func TestEditorConfig_UnknownFieldsIgnored(t *testing.T) {
	jsonData := `{
		"backup_enabled": true,
		"unknown_field": "ignored"
	}`

	var cfg EditorConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.BackupEnabled != true {
		t.Errorf("BackupEnabled: got %v, want true", cfg.BackupEnabled)
	}
}

func TestSecurityConfig_UnknownFieldsIgnored(t *testing.T) {
	jsonData := `{
		"enable_credential_scanning": true,
		"unknown_field": 123
	}`

	var cfg SecurityConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.EnableCredentialScanning != true {
		t.Errorf("EnableCredentialScanning: got %v, want true", cfg.EnableCredentialScanning)
	}
}

func TestUIConfig_UnknownFieldsIgnored(t *testing.T) {
	jsonData := `{
		"skip_prompts": true,
		"unknown_field": "ignored"
	}`

	var cfg UIConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.SkipPrompts != true {
		t.Errorf("SkipPrompts: got %v, want true", cfg.SkipPrompts)
	}
}

// --- Null Value Tests ---

func TestProviderConfig_NullValues(t *testing.T) {
	jsonData := `{
		"name": null,
		"model": null,
		"temperature": null,
		"max_tokens": null,
		"timeout": null,
		"enabled": null,
		"base_url": null,
		"api_key": null
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Null values should result in zero values
	if cfg.Name != "" {
		t.Errorf("Name: got %q, want empty string", cfg.Name)
	}
	if cfg.Model != "" {
		t.Errorf("Model: got %q, want empty string", cfg.Model)
	}
	if cfg.Temperature != 0 {
		t.Errorf("Temperature: got %f, want 0", cfg.Temperature)
	}
	if cfg.MaxTokens != 0 {
		t.Errorf("MaxTokens: got %d, want 0", cfg.MaxTokens)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout: got %d, want 0", cfg.Timeout)
	}
	if cfg.Enabled != false {
		t.Errorf("Enabled: got %v, want false", cfg.Enabled)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL: got %q, want empty string", cfg.BaseURL)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey: got %q, want empty string", cfg.APIKey)
	}
}

func TestEditorConfig_NullSliceVsEmpty(t *testing.T) {
	// Test null slice
	jsonDataNull := `{"ignore_patterns": null}`
	var cfgNull EditorConfig
	err := json.Unmarshal([]byte(jsonDataNull), &cfgNull)
	if err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}

	// Test empty array
	jsonDataEmpty := `{"ignore_patterns": []}`
	var cfgEmpty EditorConfig
	err = json.Unmarshal([]byte(jsonDataEmpty), &cfgEmpty)
	if err != nil {
		t.Fatalf("Unmarshal empty failed: %v", err)
	}

	// Null unmarshals to nil, empty array unmarshals to empty slice (not nil)
	if cfgNull.IgnorePatterns != nil {
		t.Errorf("Null slice: got %v, want nil", cfgNull.IgnorePatterns)
	}
	// Empty array results in empty slice, not nil
	if cfgEmpty.IgnorePatterns == nil {
		t.Errorf("Empty array: got nil, want empty slice")
	}
	if len(cfgEmpty.IgnorePatterns) != 0 {
		t.Errorf("Empty array length: got %d, want 0", len(cfgEmpty.IgnorePatterns))
	}
}

func TestSecurityConfig_NullSliceVsEmpty(t *testing.T) {
	// Test null slices
	jsonDataNull := `{
		"blocked_patterns": null,
		"allowed_commands": null
	}`
	var cfgNull SecurityConfig
	err := json.Unmarshal([]byte(jsonDataNull), &cfgNull)
	if err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}

	// Test empty arrays
	jsonDataEmpty := `{
		"blocked_patterns": [],
		"allowed_commands": []
	}`
	var cfgEmpty SecurityConfig
	err = json.Unmarshal([]byte(jsonDataEmpty), &cfgEmpty)
	if err != nil {
		t.Fatalf("Unmarshal empty failed: %v", err)
	}

	// Null unmarshals to nil, empty arrays unmarshal to empty slices (not nil)
	if cfgNull.BlockedPatterns != nil {
		t.Errorf("Null blocked_patterns: got %v, want nil", cfgNull.BlockedPatterns)
	}
	if cfgNull.AllowedCommands != nil {
		t.Errorf("Null allowed_commands: got %v, want nil", cfgNull.AllowedCommands)
	}
	// Empty arrays result in empty slices, not nil
	if cfgEmpty.BlockedPatterns == nil {
		t.Errorf("Empty blocked_patterns: got nil, want empty slice")
	}
	if cfgEmpty.AllowedCommands == nil {
		t.Errorf("Empty allowed_commands: got nil, want empty slice")
	}
	if len(cfgEmpty.BlockedPatterns) != 0 {
		t.Errorf("Empty blocked_patterns length: got %d, want 0", len(cfgEmpty.BlockedPatterns))
	}
	if len(cfgEmpty.AllowedCommands) != 0 {
		t.Errorf("Empty allowed_commands length: got %d, want 0", len(cfgEmpty.AllowedCommands))
	}
}

// --- Duplicate Keys Tests ---

func TestProviderConfig_DuplicateKeys(t *testing.T) {
	// JSON spec says last value wins for duplicate keys
	jsonData := `{"name": "first", "model": "gpt-3", "name": "second", "model": "gpt-4"}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Last value should win
	if cfg.Name != "second" {
		t.Errorf("Name: got %q, want \"second\"", cfg.Name)
	}
	if cfg.Model != "gpt-4" {
		t.Errorf("Model: got %q, want \"gpt-4\"", cfg.Model)
	}
}

func TestAgentConfig_DuplicateKeys(t *testing.T) {
	jsonData := `{"max_retries": 1, "cost_threshold": 1.0, "max_retries": 5, "cost_threshold": 2.5}`

	var cfg AgentConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries: got %d, want 5", cfg.MaxRetries)
	}
	if cfg.CostThreshold != 2.5 {
		t.Errorf("CostThreshold: got %f, want 2.5", cfg.CostThreshold)
	}
}

// --- Copy/Clone Tests ---

func TestProviderConfig_Copy(t *testing.T) {
	original := ProviderConfig{
		Name:        "openai",
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   4096,
		Timeout:     30,
		Enabled:     true,
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "sk-test-key",
	}

	// Copy by value
	copied := original

	// Modify copy
	copied.Name = "anthropic"
	copied.Model = "claude-3"

	// Original should be unchanged
	if original.Name != "openai" {
		t.Errorf("Original Name modified: got %q, want \"openai\"", original.Name)
	}
	if original.Model != "gpt-4" {
		t.Errorf("Original Model modified: got %q, want \"gpt-4\"", original.Model)
	}
	if copied.Name != "anthropic" {
		t.Errorf("Copied Name: got %q, want \"anthropic\"", copied.Name)
	}
	if copied.Model != "claude-3" {
		t.Errorf("Copied Model: got %q, want \"claude-3\"", copied.Model)
	}
}

func TestAgentConfig_Copy(t *testing.T) {
	original := AgentConfig{
		MaxRetries:         3,
		RetryDelay:         1000,
		MaxContextRequests: 10,
		EnableValidation:   true,
		EnableCodeReview:   true,
		ValidationTimeout:  5000,
		DefaultStrategy:    "conservative",
		CostThreshold:      1.5,
	}

	copied := original
	copied.MaxRetries = 5
	copied.DefaultStrategy = "aggressive"

	if original.MaxRetries != 3 {
		t.Errorf("Original MaxRetries modified: got %d, want 3", original.MaxRetries)
	}
	if original.DefaultStrategy != "conservative" {
		t.Errorf("Original DefaultStrategy modified: got %q, want \"conservative\"", original.DefaultStrategy)
	}
}

func TestEditorConfig_Copy(t *testing.T) {
	original := EditorConfig{
		BackupEnabled:     true,
		DiffStyle:         "unified",
		AutoFormat:        true,
		PreferredLanguage: "go",
		IgnorePatterns:    []string{"*.tmp", "vendor/"},
		MaxFileSize:       1048576,
	}

	copied := original
	copied.BackupEnabled = false
	copied.PreferredLanguage = "rust"

	if original.BackupEnabled != true {
		t.Errorf("Original BackupEnabled modified: got %v, want true", original.BackupEnabled)
	}
	if original.PreferredLanguage != "go" {
		t.Errorf("Original PreferredLanguage modified: got %q, want \"go\"", original.PreferredLanguage)
	}
}

func TestSecurityConfig_Copy(t *testing.T) {
	original := SecurityConfig{
		EnableCredentialScanning: true,
		BlockedPatterns:          []string{"password=", "secret="},
		AllowedCommands:          []string{"ls", "cat"},
		RequireConfirmation:      true,
	}

	copied := original
	copied.EnableCredentialScanning = false

	if original.EnableCredentialScanning != true {
		t.Errorf("Original EnableCredentialScanning modified: got %v, want true", original.EnableCredentialScanning)
	}
}

func TestUIConfig_Copy(t *testing.T) {
	original := UIConfig{
		SkipPrompts:    true,
		ColorOutput:    true,
		VerboseLogging: false,
		ProgressBars:   true,
		OutputFormat:   "json",
	}

	copied := original
	copied.SkipPrompts = false
	copied.OutputFormat = "yaml"

	if original.SkipPrompts != true {
		t.Errorf("Original SkipPrompts modified: got %v, want true", original.SkipPrompts)
	}
	if original.OutputFormat != "json" {
		t.Errorf("Original OutputFormat modified: got %q, want \"json\"", original.OutputFormat)
	}
}

// --- Extreme Values Tests ---

func TestProviderConfig_ExtremeValues(t *testing.T) {
	jsonData := `{
		"name": "",
		"model": "",
		"temperature": 1000.0,
		"max_tokens": 1000000000,
		"timeout": 2147483647,
		"enabled": true,
		"base_url": "",
		"api_key": ""
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Temperature != 1000.0 {
		t.Errorf("Temperature: got %f, want 1000.0", cfg.Temperature)
	}
	if cfg.MaxTokens != 1000000000 {
		t.Errorf("MaxTokens: got %d, want 1000000000", cfg.MaxTokens)
	}
	if cfg.Timeout != 2147483647 {
		t.Errorf("Timeout: got %d, want 2147483647", cfg.Timeout)
	}
}

func TestAgentConfig_ExtremeValues(t *testing.T) {
	jsonData := `{
		"max_retries": 2147483647,
		"retry_delay": 2147483647,
		"max_context_requests": 1000000,
		"validation_timeout": 2147483647,
		"cost_threshold": 999999.99
	}`

	var cfg AgentConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.MaxRetries != 2147483647 {
		t.Errorf("MaxRetries: got %d, want 2147483647", cfg.MaxRetries)
	}
	if cfg.RetryDelay != 2147483647 {
		t.Errorf("RetryDelay: got %d, want 2147483647", cfg.RetryDelay)
	}
	if cfg.MaxContextRequests != 1000000 {
		t.Errorf("MaxContextRequests: got %d, want 1000000", cfg.MaxContextRequests)
	}
	if cfg.CostThreshold != 999999.99 {
		t.Errorf("CostThreshold: got %f, want 999999.99", cfg.CostThreshold)
	}
}

func TestEditorConfig_ExtremeValues(t *testing.T) {
	jsonData := `{
		"max_file_size": 2147483647,
		"ignore_patterns": ["a", "b", "c", "d", "e"]
	}`

	var cfg EditorConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.MaxFileSize != 2147483647 {
		t.Errorf("MaxFileSize: got %d, want 2147483647", cfg.MaxFileSize)
	}
	if len(cfg.IgnorePatterns) != 5 {
		t.Errorf("IgnorePatterns length: got %d, want 5", len(cfg.IgnorePatterns))
	}
}

func TestSecurityConfig_ExtremeValues(t *testing.T) {
	jsonData := `{
		"blocked_patterns": ["pattern1", "pattern2", "pattern3", "pattern4", "pattern5", "pattern6", "pattern7", "pattern8", "pattern9", "pattern10"],
		"allowed_commands": ["cmd1", "cmd2", "cmd3", "cmd4", "cmd5"]
	}`

	var cfg SecurityConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(cfg.BlockedPatterns) != 10 {
		t.Errorf("BlockedPatterns length: got %d, want 10", len(cfg.BlockedPatterns))
	}
	if len(cfg.AllowedCommands) != 5 {
		t.Errorf("AllowedCommands length: got %d, want 5", len(cfg.AllowedCommands))
	}
}

func TestUIConfig_ExtremeValues(t *testing.T) {
	jsonData := `{
		"output_format": "very-long-output-format-string-that-goes-on-and-on-and-on"
	}`

	var cfg UIConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.OutputFormat != "very-long-output-format-string-that-goes-on-and-on-and-on" {
		t.Errorf("OutputFormat: got %q, want long string", cfg.OutputFormat)
	}
}

// --- Unicode and Special Characters Tests ---

func TestProviderConfig_UnicodeValues(t *testing.T) {
	jsonData := `{
		"name": "测试提供者",
		"model": "模型 - 测试",
		"base_url": "https://api.测试.com/v1",
		"api_key": "sk-тест-ключ"
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Name != "测试提供者" {
		t.Errorf("Name: got %q, want \"测试提供者\"", cfg.Name)
	}
	if cfg.Model != "模型 - 测试" {
		t.Errorf("Model: got %q, want \"模型 - 测试\"", cfg.Model)
	}
	if cfg.BaseURL != "https://api.测试.com/v1" {
		t.Errorf("BaseURL: got %q, want \"https://api.测试.com/v1\"", cfg.BaseURL)
	}
	if cfg.APIKey != "sk-тест-ключ" {
		t.Errorf("APIKey: got %q, want \"sk-тест-ключ\"", cfg.APIKey)
	}
}

func TestEditorConfig_UnicodePatterns(t *testing.T) {
	jsonData := `{
		"ignore_patterns": ["*.测试", "dist/构建", "node_modules/"]
	}`

	var cfg EditorConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(cfg.IgnorePatterns) != 3 {
		t.Errorf("IgnorePatterns length: got %d, want 3", len(cfg.IgnorePatterns))
	}
	if cfg.IgnorePatterns[0] != "*.测试" {
		t.Errorf("IgnorePatterns[0]: got %q, want \"*.测试\"", cfg.IgnorePatterns[0])
	}
	if cfg.IgnorePatterns[1] != "dist/构建" {
		t.Errorf("IgnorePatterns[1]: got %q, want \"dist/构建\"", cfg.IgnorePatterns[1])
	}
}

// --- JSON Tag Behavior Tests ---

func TestProviderConfig_JSONTagCase(t *testing.T) {
	// Test that JSON tags use camelCase
	cfg := ProviderConfig{Name: "test", Model: "test-model"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify camelCase in JSON
	expected := `{"name":"test","model":"test-model","temperature":0,"max_tokens":0,"timeout":0,"enabled":false}`
	if string(data) != expected {
		t.Errorf("JSON tag case:\ngot:  %s\nwant: %s", string(data), expected)
	}
}

func TestAgentConfig_JSONTagCase(t *testing.T) {
	cfg := AgentConfig{MaxRetries: 3, CostThreshold: 1.5}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify camelCase in JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Check that keys are camelCase, not snake_case
	if _, exists := raw["max_retries"]; !exists {
		t.Error("Expected camelCase key 'max_retries' in JSON")
	}
	if _, exists := raw["maxRetries"]; exists {
		t.Error("Got snake_case key 'maxRetries', expected camelCase 'max_retries'")
	}
}

func TestEditorConfig_JSONTagCase(t *testing.T) {
	cfg := EditorConfig{BackupEnabled: true, MaxFileSize: 1048576}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify camelCase keys exist
	if _, exists := raw["backup_enabled"]; !exists {
		t.Error("Expected camelCase key 'backup_enabled' in JSON")
	}
	if _, exists := raw["max_file_size"]; !exists {
		t.Error("Expected camelCase key 'max_file_size' in JSON")
	}
}

func TestSecurityConfig_JSONTagCase(t *testing.T) {
	cfg := SecurityConfig{EnableCredentialScanning: true, RequireConfirmation: true}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify camelCase keys exist
	if _, exists := raw["enable_credential_scanning"]; !exists {
		t.Error("Expected camelCase key 'enable_credential_scanning' in JSON")
	}
	if _, exists := raw["require_confirmation"]; !exists {
		t.Error("Expected camelCase key 'require_confirmation' in JSON")
	}
}

func TestUIConfig_JSONTagCase(t *testing.T) {
	cfg := UIConfig{SkipPrompts: true, ColorOutput: true}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify camelCase keys exist
	if _, exists := raw["skip_prompts"]; !exists {
		t.Error("Expected camelCase key 'skip_prompts' in JSON")
	}
	if _, exists := raw["color_output"]; !exists {
		t.Error("Expected camelCase key 'color_output' in JSON")
	}
}

// --- Empty and Whitespace Tests ---

func TestProviderConfig_EmptyStringValues(t *testing.T) {
	jsonData := `{
		"name": "",
		"model": "",
		"base_url": "",
		"api_key": ""
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Name != "" {
		t.Errorf("Name: got %q, want empty string", cfg.Name)
	}
	if cfg.Model != "" {
		t.Errorf("Model: got %q, want empty string", cfg.Model)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL: got %q, want empty string", cfg.BaseURL)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey: got %q, want empty string", cfg.APIKey)
	}
}

func TestUIConfig_EmptyStringOutputFormat(t *testing.T) {
	jsonData := `{"output_format": ""}`

	var cfg UIConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.OutputFormat != "" {
		t.Errorf("OutputFormat: got %q, want empty string", cfg.OutputFormat)
	}
}

// --- Whitespace Handling Tests ---

func TestProviderConfig_WhitespaceHandling(t *testing.T) {
	jsonData := `  {
		"name" : "test-provider",
		"model" : "test-model"
	}  `

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Name != "test-provider" {
		t.Errorf("Name: got %q, want \"test-provider\"", cfg.Name)
	}
	if cfg.Model != "test-model" {
		t.Errorf("Model: got %q, want \"test-model\"", cfg.Model)
	}
}

// --- Nested Object Tests ---

func TestProviderConfig_NestedObjectIgnored(t *testing.T) {
	jsonData := `{
		"name": "test",
		"model": "test-model",
		"nested": {
			"key": "value",
			"nested": {
				"deep": true
			}
		}
	}`

	var cfg ProviderConfig
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Nested objects should be silently ignored
	if cfg.Name != "test" {
		t.Errorf("Name: got %q, want \"test\"", cfg.Name)
	}
}

// --- Array Element Types Tests ---

func TestEditorConfig_IgnorePatternsArrayElementTypes(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"all strings", `{"ignore_patterns": ["*.tmp", "dist/"]}`, false},
		{"mixed types (string and number)", `{"ignore_patterns": ["*.tmp", 123]}`, true},
		{"mixed types (string and object)", `{"ignore_patterns": ["*.tmp", {"key": "value"}]}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg EditorConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityConfig_SliceElementTypes(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"all strings", `{"blocked_patterns": ["pattern1", "pattern2"]}`, false},
		{"mixed types", `{"blocked_patterns": ["pattern1", 123]}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg SecurityConfig
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
