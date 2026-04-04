package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

// --- ProviderConfig Tests ---

func TestProviderConfig_JSONRoundTrip(t *testing.T) {
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

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ProviderConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Errorf("Round-trip mismatch.\nWant: %+v\nGot:  %+v", original, decoded)
	}
}

func TestProviderConfig_ZeroValues(t *testing.T) {
	var cfg ProviderConfig

	if cfg.Name != "" {
		t.Errorf("Name zero value: got %q, want empty string", cfg.Name)
	}
	if cfg.Model != "" {
		t.Errorf("Model zero value: got %q, want empty string", cfg.Model)
	}
	if cfg.Temperature != 0 {
		t.Errorf("Temperature zero value: got %f, want 0", cfg.Temperature)
	}
	if cfg.MaxTokens != 0 {
		t.Errorf("MaxTokens zero value: got %d, want 0", cfg.MaxTokens)
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout zero value: got %d, want 0", cfg.Timeout)
	}
	if cfg.Enabled != false {
		t.Errorf("Enabled zero value: got %v, want false", cfg.Enabled)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL zero value: got %q, want empty string", cfg.BaseURL)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey zero value: got %q, want empty string", cfg.APIKey)
	}
}

func TestProviderConfig_PartialUnmarshal(t *testing.T) {
	jsonData := `{"name":"anthropic","model":"claude-3"}`

	var cfg ProviderConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Provided fields
	if cfg.Name != "anthropic" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "anthropic")
	}
	if cfg.Model != "claude-3" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "claude-3")
	}

	// Missing fields should be zero values
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

func TestProviderConfig_Omitempty_BaseURL(t *testing.T) {
	// BaseURL empty → should be omitted from JSON
	cfg := ProviderConfig{Name: "test", Model: "test-model"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	if _, exists := raw["base_url"]; exists {
		t.Error("base_url should be omitted when empty (omitempty)")
	}

	// BaseURL set → should appear in JSON
	cfg.BaseURL = "https://example.com"
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal with BaseURL failed: %v", err)
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if val, exists := raw["base_url"]; !exists || val != "https://example.com" {
		t.Errorf("base_url: got %v, want present with value 'https://example.com'", val)
	}
}

func TestProviderConfig_Omitempty_APIKey(t *testing.T) {
	// APIKey empty → should be omitted
	cfg := ProviderConfig{Name: "test", Model: "test-model"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	if _, exists := raw["api_key"]; exists {
		t.Error("api_key should be omitted when empty (omitempty)")
	}

	// APIKey set → should appear
	cfg.APIKey = "sk-secret"
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal with APIKey failed: %v", err)
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if val, exists := raw["api_key"]; !exists || val != "sk-secret" {
		t.Errorf("api_key: got %v, want present with value 'sk-secret'", val)
	}
}

// --- AgentConfig Tests ---

func TestAgentConfig_JSONRoundTrip(t *testing.T) {
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

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded AgentConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Errorf("Round-trip mismatch.\nWant: %+v\nGot:  %+v", original, decoded)
	}
}

func TestAgentConfig_ZeroValues(t *testing.T) {
	var cfg AgentConfig

	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries zero value: got %d, want 0", cfg.MaxRetries)
	}
	if cfg.RetryDelay != 0 {
		t.Errorf("RetryDelay zero value: got %d, want 0", cfg.RetryDelay)
	}
	if cfg.MaxContextRequests != 0 {
		t.Errorf("MaxContextRequests zero value: got %d, want 0", cfg.MaxContextRequests)
	}
	if cfg.EnableValidation != false {
		t.Errorf("EnableValidation zero value: got %v, want false", cfg.EnableValidation)
	}
	if cfg.EnableCodeReview != false {
		t.Errorf("EnableCodeReview zero value: got %v, want false", cfg.EnableCodeReview)
	}
	if cfg.ValidationTimeout != 0 {
		t.Errorf("ValidationTimeout zero value: got %d, want 0", cfg.ValidationTimeout)
	}
	if cfg.DefaultStrategy != "" {
		t.Errorf("DefaultStrategy zero value: got %q, want empty string", cfg.DefaultStrategy)
	}
	if cfg.CostThreshold != 0 {
		t.Errorf("CostThreshold zero value: got %f, want 0", cfg.CostThreshold)
	}
}

func TestAgentConfig_PartialUnmarshal(t *testing.T) {
	jsonData := `{"max_retries":5,"cost_threshold":2.5}`

	var cfg AgentConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries: got %d, want 5", cfg.MaxRetries)
	}
	if cfg.CostThreshold != 2.5 {
		t.Errorf("CostThreshold: got %f, want 2.5", cfg.CostThreshold)
	}

	// Missing fields → zero values
	if cfg.EnableValidation != false {
		t.Errorf("EnableValidation: got %v, want false", cfg.EnableValidation)
	}
	if cfg.DefaultStrategy != "" {
		t.Errorf("DefaultStrategy: got %q, want empty string", cfg.DefaultStrategy)
	}
	if cfg.RetryDelay != 0 {
		t.Errorf("RetryDelay: got %d, want 0", cfg.RetryDelay)
	}
}

// --- EditorConfig Tests ---

func TestEditorConfig_JSONRoundTrip(t *testing.T) {
	original := EditorConfig{
		BackupEnabled:     true,
		DiffStyle:         "unified",
		AutoFormat:        true,
		PreferredLanguage: "go",
		IgnorePatterns:    []string{"*.tmp", "*.bak", "vendor/"},
		MaxFileSize:       1048576,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded EditorConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(decoded, original) {
		t.Errorf("Round-trip mismatch.\nWant: %+v\nGot:  %+v", original, decoded)
	}

	_ = data // used above
}

func TestEditorConfig_ZeroValues(t *testing.T) {
	var cfg EditorConfig

	if cfg.BackupEnabled != false {
		t.Errorf("BackupEnabled zero value: got %v, want false", cfg.BackupEnabled)
	}
	if cfg.DiffStyle != "" {
		t.Errorf("DiffStyle zero value: got %q, want empty string", cfg.DiffStyle)
	}
	if cfg.AutoFormat != false {
		t.Errorf("AutoFormat zero value: got %v, want false", cfg.AutoFormat)
	}
	if cfg.PreferredLanguage != "" {
		t.Errorf("PreferredLanguage zero value: got %q, want empty string", cfg.PreferredLanguage)
	}
	if cfg.IgnorePatterns != nil {
		t.Errorf("IgnorePatterns zero value: got %v, want nil", cfg.IgnorePatterns)
	}
	if cfg.MaxFileSize != 0 {
		t.Errorf("MaxFileSize zero value: got %d, want 0", cfg.MaxFileSize)
	}
}

func TestEditorConfig_SliceFieldMarshaling(t *testing.T) {
	tests := []struct {
		name   string
		patterns []string
	}{
		{"multiple patterns", []string{"*.log", "dist/", "node_modules/"}},
		{"single pattern", []string{"*.tmp"}},
		{"empty slice", []string{}},
		{"nil slice", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := EditorConfig{IgnorePatterns: tt.patterns}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded EditorConfig
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			// nil and empty slice both unmarshal to nil from JSON "[]"
			if tt.patterns != nil {
				if len(decoded.IgnorePatterns) != len(tt.patterns) {
					t.Errorf("IgnorePatterns length: got %d, want %d", len(decoded.IgnorePatterns), len(tt.patterns))
				}
				for i, p := range tt.patterns {
					if decoded.IgnorePatterns[i] != p {
						t.Errorf("IgnorePatterns[%d]: got %q, want %q", i, decoded.IgnorePatterns[i], p)
					}
				}
			}
		})
	}
}

func TestEditorConfig_PartialUnmarshal(t *testing.T) {
	jsonData := `{"backup_enabled":true,"max_file_size":2048}`

	var cfg EditorConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.BackupEnabled != true {
		t.Errorf("BackupEnabled: got %v, want true", cfg.BackupEnabled)
	}
	if cfg.MaxFileSize != 2048 {
		t.Errorf("MaxFileSize: got %d, want 2048", cfg.MaxFileSize)
	}

	// Missing fields → zero values
	if cfg.AutoFormat != false {
		t.Errorf("AutoFormat: got %v, want false", cfg.AutoFormat)
	}
	if cfg.DiffStyle != "" {
		t.Errorf("DiffStyle: got %q, want empty string", cfg.DiffStyle)
	}
	if cfg.IgnorePatterns != nil {
		t.Errorf("IgnorePatterns: got %v, want nil", cfg.IgnorePatterns)
	}
}

// --- SecurityConfig Tests ---

func TestSecurityConfig_JSONRoundTrip(t *testing.T) {
	original := SecurityConfig{
		EnableCredentialScanning: true,
		BlockedPatterns:          []string{"password=", "secret=", "api_key="},
		AllowedCommands:          []string{"ls", "cat", "grep"},
		RequireConfirmation:      true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SecurityConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(decoded, original) {
		t.Errorf("Round-trip mismatch.\nWant: %+v\nGot:  %+v", original, decoded)
	}

	_ = data // used above
}

func TestSecurityConfig_ZeroValues(t *testing.T) {
	var cfg SecurityConfig

	if cfg.EnableCredentialScanning != false {
		t.Errorf("EnableCredentialScanning zero value: got %v, want false", cfg.EnableCredentialScanning)
	}
	if cfg.BlockedPatterns != nil {
		t.Errorf("BlockedPatterns zero value: got %v, want nil", cfg.BlockedPatterns)
	}
	if cfg.AllowedCommands != nil {
		t.Errorf("AllowedCommands zero value: got %v, want nil", cfg.AllowedCommands)
	}
	if cfg.RequireConfirmation != false {
		t.Errorf("RequireConfirmation zero value: got %v, want false", cfg.RequireConfirmation)
	}
}

func TestSecurityConfig_SliceFields(t *testing.T) {
	cfg := SecurityConfig{
		BlockedPatterns: []string{"*.pem", "*.key"},
		AllowedCommands: []string{"echo", "pwd"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SecurityConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.BlockedPatterns) != 2 {
		t.Errorf("BlockedPatterns length: got %d, want 2", len(decoded.BlockedPatterns))
	}
	if len(decoded.AllowedCommands) != 2 {
		t.Errorf("AllowedCommands length: got %d, want 2", len(decoded.AllowedCommands))
	}
}

func TestSecurityConfig_PartialUnmarshal(t *testing.T) {
	jsonData := `{"enable_credential_scanning":true}`

	var cfg SecurityConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.EnableCredentialScanning != true {
		t.Errorf("EnableCredentialScanning: got %v, want true", cfg.EnableCredentialScanning)
	}
	if cfg.RequireConfirmation != false {
		t.Errorf("RequireConfirmation: got %v, want false", cfg.RequireConfirmation)
	}
	if cfg.BlockedPatterns != nil {
		t.Errorf("BlockedPatterns: got %v, want nil", cfg.BlockedPatterns)
	}
	if cfg.AllowedCommands != nil {
		t.Errorf("AllowedCommands: got %v, want nil", cfg.AllowedCommands)
	}
}

// --- UIConfig Tests ---

func TestUIConfig_JSONRoundTrip(t *testing.T) {
	original := UIConfig{
		SkipPrompts:    true,
		ColorOutput:    true,
		VerboseLogging: false,
		ProgressBars:   true,
		OutputFormat:   "json",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded UIConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Errorf("Round-trip mismatch.\nWant: %+v\nGot:  %+v", original, decoded)
	}
}

func TestUIConfig_ZeroValues(t *testing.T) {
	var cfg UIConfig

	if cfg.SkipPrompts != false {
		t.Errorf("SkipPrompts zero value: got %v, want false", cfg.SkipPrompts)
	}
	if cfg.ColorOutput != false {
		t.Errorf("ColorOutput zero value: got %v, want false", cfg.ColorOutput)
	}
	if cfg.VerboseLogging != false {
		t.Errorf("VerboseLogging zero value: got %v, want false", cfg.VerboseLogging)
	}
	if cfg.ProgressBars != false {
		t.Errorf("ProgressBars zero value: got %v, want false", cfg.ProgressBars)
	}
	if cfg.OutputFormat != "" {
		t.Errorf("OutputFormat zero value: got %q, want empty string", cfg.OutputFormat)
	}
}

func TestUIConfig_BooleanDefaults(t *testing.T) {
	// All booleans default to false
	jsonData := `{}`

	var cfg UIConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.SkipPrompts != false {
		t.Errorf("SkipPrompts default: got %v, want false", cfg.SkipPrompts)
	}
	if cfg.ColorOutput != false {
		t.Errorf("ColorOutput default: got %v, want false", cfg.ColorOutput)
	}
	if cfg.VerboseLogging != false {
		t.Errorf("VerboseLogging default: got %v, want false", cfg.VerboseLogging)
	}
	if cfg.ProgressBars != false {
		t.Errorf("ProgressBars default: got %v, want false", cfg.ProgressBars)
	}

	// Explicitly set to true
	jsonData = `{"skip_prompts":true,"color_output":true,"verbose_logging":true,"progress_bars":true}`
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.SkipPrompts != true {
		t.Errorf("SkipPrompts: got %v, want true", cfg.SkipPrompts)
	}
	if cfg.ColorOutput != true {
		t.Errorf("ColorOutput: got %v, want true", cfg.ColorOutput)
	}
	if cfg.VerboseLogging != true {
		t.Errorf("VerboseLogging: got %v, want true", cfg.VerboseLogging)
	}
	if cfg.ProgressBars != true {
		t.Errorf("ProgressBars: got %v, want true", cfg.ProgressBars)
	}
}

func TestUIConfig_PartialUnmarshal(t *testing.T) {
	jsonData := `{"output_format":"yaml"}`

	var cfg UIConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.OutputFormat != "yaml" {
		t.Errorf("OutputFormat: got %q, want %q", cfg.OutputFormat, "yaml")
	}

	// Missing booleans → default false
	if cfg.SkipPrompts != false {
		t.Errorf("SkipPrompts: got %v, want false", cfg.SkipPrompts)
	}
	if cfg.ColorOutput != false {
		t.Errorf("ColorOutput: got %v, want false", cfg.ColorOutput)
	}
}

// --- Additional Edge Cases ---

func TestProviderConfig_EmptyJSONObject(t *testing.T) {
	var cfg ProviderConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Name != "" || cfg.Model != "" || cfg.Enabled != false {
		t.Error("Empty JSON object should produce zero-value struct")
	}
}

func TestProviderConfigNegativeValues(t *testing.T) {
	jsonData := `{"temperature":-1.0,"max_tokens":-1,"timeout":-1}`
	var cfg ProviderConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Temperature != -1.0 {
		t.Errorf("Temperature: got %f, want -1.0", cfg.Temperature)
	}
	if cfg.MaxTokens != -1 {
		t.Errorf("MaxTokens: got %d, want -1", cfg.MaxTokens)
	}
	if cfg.Timeout != -1 {
		t.Errorf("Timeout: got %d, want -1", cfg.Timeout)
	}
}
