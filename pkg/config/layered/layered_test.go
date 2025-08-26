package layered

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
)

func TestLayeredConfiguration(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "layered-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("DefaultsConfigSource", func(t *testing.T) {
		source := NewDefaultsConfigSource("test-defaults", 0)

		if source.GetName() != "test-defaults" {
			t.Errorf("Expected name 'test-defaults', got %s", source.GetName())
		}

		if source.GetPriority() != 0 {
			t.Errorf("Expected priority 0, got %d", source.GetPriority())
		}

		cfg, err := source.Load(nil)
		if err != nil {
			t.Errorf("Failed to load defaults: %v", err)
		}

		if cfg == nil {
			t.Error("Expected non-nil config")
		}

		if cfg.LLM == nil {
			t.Error("Expected LLM config to be set")
		}
	})

	t.Run("FileConfigSource", func(t *testing.T) {
		// Create a test config file
		configPath := filepath.Join(tempDir, "test-config.json")
		configContent := `{
			"llm": {
				"editing_model": "test-model",
				"temperature": 0.5
			}
		}`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		source := NewFileConfigSource(configPath, "test-file", 10, false)

		if source.GetName() != "test-file" {
			t.Errorf("Expected name 'test-file', got %s", source.GetName())
		}

		cfg, err := source.Load(nil)
		if err != nil {
			t.Errorf("Failed to load file config: %v", err)
		}

		if cfg == nil {
			t.Error("Expected non-nil config")
		}

		if cfg.LLM.EditingModel != "test-model" {
			t.Errorf("Expected editing model 'test-model', got %s", cfg.LLM.EditingModel)
		}
	})

	t.Run("EnvironmentConfigSource", func(t *testing.T) {
		// Set test environment variables
		os.Setenv("TEST_TEMPERATURE", "0.8")
		os.Setenv("TEST_SKIP_PROMPTS", "true")
		defer func() {
			os.Unsetenv("TEST_TEMPERATURE")
			os.Unsetenv("TEST_SKIP_PROMPTS")
		}()

		source := NewEnvironmentConfigSource("TEST_", "test-env", 20)

		cfg, err := source.Load(nil)
		if err != nil {
			t.Errorf("Failed to load environment config: %v", err)
		}

		if cfg.LLM.Temperature != 0.8 {
			t.Errorf("Expected temperature 0.8, got %f", cfg.LLM.Temperature)
		}

		if !cfg.SkipPrompt {
			t.Error("Expected SkipPrompt to be true")
		}
	})

	t.Run("LayeredConfigLoader", func(t *testing.T) {
		loader := NewLayeredConfigLoader()

		// Add defaults
		defaultsSource := NewDefaultsConfigSource("defaults", 0)
		err := loader.AddConfigSource(defaultsSource)
		if err != nil {
			t.Errorf("Failed to add defaults source: %v", err)
		}

		// Add environment override
		os.Setenv("TEST_TEMPERATURE", "0.9")
		defer os.Unsetenv("TEST_TEMPERATURE")

		envSource := NewEnvironmentConfigSource("TEST_", "environment", 10)
		err = loader.AddConfigSource(envSource)
		if err != nil {
			t.Errorf("Failed to add environment source: %v", err)
		}

		// Get merged config
		merged := loader.GetMergedConfig()
		if merged == nil {
			t.Error("Expected non-nil merged config")
		}

		// Environment should override defaults
		if merged.LLM.Temperature != 0.9 {
			t.Errorf("Expected temperature 0.9 (from env), got %f", merged.LLM.Temperature)
		}
	})

	t.Run("LayeredConfigProvider", func(t *testing.T) {
		provider := NewLayeredConfigProvider()

		// Add defaults
		defaultsSource := NewDefaultsConfigSource("defaults", 0)
		err := provider.AddSource(defaultsSource)
		if err != nil {
			t.Errorf("Failed to add defaults source: %v", err)
		}

		// Test getting merged config
		merged := provider.GetMergedConfig()
		if merged == nil {
			t.Error("Expected non-nil merged config")
		}

		// Test getting agent config
		agentConfig := provider.GetAgentConfig()
		if agentConfig == nil {
			t.Error("Expected non-nil agent config")
		}

		// Test getting UI config
		uiConfig := provider.GetUIConfig()
		if uiConfig == nil {
			t.Error("Expected non-nil UI config")
		}

		// Test getting security config
		securityConfig := provider.GetSecurityConfig()
		if securityConfig == nil {
			t.Error("Expected non-nil security config")
		}
	})
}

func TestConfigurationValidation(t *testing.T) {
	t.Run("LLMModelValidationRule", func(t *testing.T) {
		rule := &LLMModelValidationRule{}

		// Test valid config
		validConfig := config.DefaultConfig()
		errors := rule.Validate(validConfig)

		if len(errors) > 0 {
			t.Errorf("Expected no errors for valid config, got %d errors:", len(errors))
			for _, err := range errors {
				t.Errorf("  - %s: %s", err.Code, err.Message)
			}
		}

		// Test invalid config
		invalidConfig := config.DefaultConfig()
		invalidConfig.LLM.EditingModel = ""  // Invalid
		invalidConfig.LLM.Temperature = -1.0 // Invalid

		errors = rule.Validate(invalidConfig)
		if len(errors) == 0 {
			t.Error("Expected errors for invalid config")
		}

		// Should have specific error codes
		hasModelError := false
		hasTempError := false
		for _, err := range errors {
			if err.Code == "MISSING_EDITING_MODEL" {
				hasModelError = true
			}
			if err.Code == "INVALID_TEMPERATURE_RANGE" {
				hasTempError = true
			}
		}

		if !hasModelError {
			t.Error("Expected missing editing model error")
		}
		if !hasTempError {
			t.Error("Expected invalid temperature error")
		}
	})

	t.Run("ConfigValidator", func(t *testing.T) {
		validator := CreateStandardValidator()

		// Test valid config
		validConfig := config.DefaultConfig()
		result := validator.ValidateConfig(validConfig)

		if result == nil {
			t.Error("Expected non-nil validation result")
		}

		if len(result.Errors) > 0 {
			t.Errorf("Expected no errors for valid config, got %d errors:", len(result.Errors))
			for _, err := range result.Errors {
				t.Errorf("  - %s: %s", err.Code, err.Message)
			}
		}

		// Test invalid config
		invalidConfig := config.DefaultConfig()
		invalidConfig.LLM.Temperature = 5.0 // Too high

		result = validator.ValidateConfig(invalidConfig)
		if result.IsValid() {
			t.Error("Expected invalid config to fail validation")
		}

		if len(result.Errors) == 0 {
			t.Error("Expected validation errors")
		}
	})
}

func TestConfigurationFactory(t *testing.T) {
	t.Run("CreateStandardSetup", func(t *testing.T) {
		factory := NewConfigurationFactory()

		provider, err := factory.CreateStandardSetup()
		if err != nil {
			t.Errorf("Failed to create standard setup: %v", err)
		}

		if provider == nil {
			t.Error("Expected non-nil provider")
		}

		// Should be able to get merged config
		config := provider.GetMergedConfig()
		if config == nil {
			t.Error("Expected non-nil merged config")
		}
	})
}

func BenchmarkLayeredConfig(b *testing.B) {
	provider := NewLayeredConfigProvider()
	defaultsSource := NewDefaultsConfigSource("defaults", 0)
	provider.AddSource(defaultsSource)

	b.Run("GetMergedConfig", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			config := provider.GetMergedConfig()
			if config == nil {
				b.Fatal("Expected non-nil config")
			}
		}
	})

	b.Run("GetAgentConfig", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			config := provider.GetAgentConfig()
			if config == nil {
				b.Fatal("Expected non-nil agent config")
			}
		}
	})

	validator := CreateStandardValidator()
	testConfig := config.DefaultConfig()

	b.Run("ValidateConfig", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result := validator.ValidateConfig(testConfig)
			if result == nil {
				b.Fatal("Expected non-nil validation result")
			}
		}
	})
}
