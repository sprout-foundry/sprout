package cmd

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// =============================================================================
// getTraceDatasetDir
// =============================================================================

func TestGetTraceDatasetDir_EmptyFlag(t *testing.T) {
	// Clear env var and test with empty flag
	orig := os.Getenv("LEDIT_TRACE_DATASET_DIR")
	os.Unsetenv("LEDIT_TRACE_DATASET_DIR")
	defer os.Setenv("LEDIT_TRACE_DATASET_DIR", orig)

	got := getTraceDatasetDir("")
	if got != "" {
		t.Errorf("getTraceDatasetDir(\"\") = %q, want empty", got)
	}
}

func TestGetTraceDatasetDir_NonEmptyFlag(t *testing.T) {
	orig := os.Getenv("LEDIT_TRACE_DATASET_DIR")
	os.Unsetenv("LEDIT_TRACE_DATASET_DIR")
	defer os.Setenv("LEDIT_TRACE_DATASET_DIR", orig)

	got := getTraceDatasetDir("/tmp/traces")
	if got != "/tmp/traces" {
		t.Errorf("getTraceDatasetDir(\"/tmp/traces\") = %q, want \"/tmp/traces\"", got)
	}
}

func TestGetTraceDatasetDir_EnvVar(t *testing.T) {
	orig := os.Getenv("LEDIT_TRACE_DATASET_DIR")
	os.Setenv("LEDIT_TRACE_DATASET_DIR", "/env/traces")
	defer os.Setenv("LEDIT_TRACE_DATASET_DIR", orig)

	got := getTraceDatasetDir("")
	if got != "/env/traces" {
		t.Errorf("getTraceDatasetDir(\"\") with env set = %q, want \"/env/traces\"", got)
	}
}

func TestGetTraceDatasetDir_EnvVarEmpty(t *testing.T) {
	orig := os.Getenv("LEDIT_TRACE_DATASET_DIR")
	os.Setenv("LEDIT_TRACE_DATASET_DIR", "")
	defer os.Setenv("LEDIT_TRACE_DATASET_DIR", orig)

	got := getTraceDatasetDir("")
	if got != "" {
		t.Errorf("getTraceDatasetDir(\"\") with env empty = %q, want empty", got)
	}
}

func TestGetTraceDatasetDir_FlagTakesPriority(t *testing.T) {
	orig := os.Getenv("LEDIT_TRACE_DATASET_DIR")
	os.Setenv("LEDIT_TRACE_DATASET_DIR", "/env/traces")
	defer os.Setenv("LEDIT_TRACE_DATASET_DIR", orig)

	got := getTraceDatasetDir("/flag/traces")
	if got != "/flag/traces" {
		t.Errorf("getTraceDatasetDir(\"/flag/traces\") with env set = %q, want \"/flag/traces\"", got)
	}
}

// =============================================================================
// getProviderFromConfig
// =============================================================================

func TestGetProviderFromConfig_NilConfig(t *testing.T) {
	got := getProviderFromConfig(nil)
	if got != "" {
		t.Errorf("getProviderFromConfig(nil) = %q, want empty", got)
	}
}

func TestGetProviderFromConfig_LastUsedProvider(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
	}
	got := getProviderFromConfig(cfg)
	if got != "anthropic" {
		t.Errorf("getProviderFromConfig with LastUsedProvider = %q, want \"anthropic\"", got)
	}
}

func TestGetProviderFromConfig_ProviderPriority(t *testing.T) {
	cfg := &configuration.Config{
		ProviderPriority: []string{"openai", "anthropic", "google"},
	}
	got := getProviderFromConfig(cfg)
	if got != "openai" {
		t.Errorf("getProviderFromConfig with ProviderPriority = %q, want \"openai\"", got)
	}
}

func TestGetProviderFromConfig_LastUsedTakesPriority(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
		ProviderPriority: []string{"openai", "google"},
	}
	got := getProviderFromConfig(cfg)
	if got != "anthropic" {
		t.Errorf("getProviderFromConfig should prefer LastUsedProvider, got %q, want \"anthropic\"", got)
	}
}

func TestGetProviderFromConfig_EmptyConfig(t *testing.T) {
	cfg := &configuration.Config{}
	got := getProviderFromConfig(cfg)
	if got != "" {
		t.Errorf("getProviderFromConfig(empty Config) = %q, want empty", got)
	}
}

// =============================================================================
// getModelFromConfig
// =============================================================================

func TestGetModelFromConfig_FlagValue(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
		ProviderModels:   map[string]string{"anthropic": "claude-3-opus"},
	}
	got := getModelFromConfig(cfg, "my-custom-model")
	if got != "my-custom-model" {
		t.Errorf("getModelFromConfig with flag = %q, want \"my-custom-model\"", got)
	}
}

func TestGetModelFromConfig_NilConfig(t *testing.T) {
	got := getModelFromConfig(nil, "")
	if got != "" {
		t.Errorf("getModelFromConfig(nil, \"\") = %q, want empty", got)
	}
}

func TestGetModelFromConfig_NilConfigWithFlag(t *testing.T) {
	got := getModelFromConfig(nil, "flag-model")
	if got != "flag-model" {
		t.Errorf("getModelFromConfig(nil, \"flag-model\") = %q, want \"flag-model\"", got)
	}
}

func TestGetModelFromConfig_MatchingProvider(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "openai",
		ProviderModels:   map[string]string{"openai": "gpt-4o", "anthropic": "claude-3-opus"},
	}
	got := getModelFromConfig(cfg, "")
	if got != "gpt-4o" {
		t.Errorf("getModelFromConfig with matching provider = %q, want \"gpt-4o\"", got)
	}
}

func TestGetModelFromConfig_NoMatchingProvider(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
		ProviderModels:   map[string]string{"openai": "gpt-4o"},
	}
	got := getModelFromConfig(cfg, "")
	if got != "" {
		t.Errorf("getModelFromConfig with no matching provider = %q, want empty", got)
	}
}

func TestGetModelFromConfig_NoProviderModels(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "anthropic",
		ProviderModels:   nil,
	}
	got := getModelFromConfig(cfg, "")
	if got != "" {
		t.Errorf("getModelFromConfig with nil ProviderModels = %q, want empty", got)
	}
}

// =============================================================================
// NewBaseCommand
// =============================================================================

func TestNewBaseCommand(t *testing.T) {
	base := NewBaseCommand("test-use", "test short", "test long description")
	if base == nil {
		t.Fatal("NewBaseCommand returned nil")
	}

	cmd := base.GetCommand()
	if cmd == nil {
		t.Fatal("GetCommand returned nil")
	}
	if cmd.Use != "test-use" {
		t.Errorf("cmd.Use = %q, want \"test-use\"", cmd.Use)
	}
	if cmd.Short != "test short" {
		t.Errorf("cmd.Short = %q, want \"test short\"", cmd.Short)
	}
	if cmd.Long != "test long description" {
		t.Errorf("cmd.Long = %q, want \"test long description\"", cmd.Long)
	}
}

func TestNewBaseCommand_HasCommonFlags(t *testing.T) {
	base := NewBaseCommand("test", "short", "long")
	cmd := base.GetCommand()

	// Verify common flags are registered
	flagNames := []string{"skip-prompt", "model", "dry-run", "trace-dataset-dir"}
	for _, name := range flagNames {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("expected flag %q to be registered", name)
		}
	}
}

func TestNewBaseCommand_FlagDefaults(t *testing.T) {
	base := NewBaseCommand("test", "short", "long")
	cmd := base.GetCommand()

	skipPrompt, _ := cmd.Flags().GetBool("skip-prompt")
	if skipPrompt {
		t.Errorf("skip-prompt default should be false")
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Errorf("dry-run default should be false")
	}

	traceDir, _ := cmd.Flags().GetString("trace-dataset-dir")
	if traceDir != "" {
		t.Errorf("trace-dataset-dir default should be empty, got %q", traceDir)
	}

	model, err := cmd.Flags().GetString("model")
	if err != nil {
		t.Fatalf("failed to get model flag: %v", err)
	}
	if model != "" {
		t.Errorf("model default should be empty, got %q", model)
	}
}

// =============================================================================
// GetCommand
// =============================================================================

func TestGetCommand(t *testing.T) {
	base := NewBaseCommand("use", "short", "long")
	cmd := base.GetCommand()
	if cmd == nil {
		t.Fatal("GetCommand should return non-nil command")
	}
}

func TestGetCommand_SameInstance(t *testing.T) {
	base := NewBaseCommand("use", "short", "long")
	cmd1 := base.GetCommand()
	cmd2 := base.GetCommand()
	if cmd1 != cmd2 {
		t.Error("GetCommand should return the same cobra.Command instance each time")
	}
}

// =============================================================================
// AddCustomFlag
// =============================================================================

func TestAddCustomFlag(t *testing.T) {
	base := NewBaseCommand("test", "short", "long")
	ptr := base.AddCustomFlag("my-flag", "m", "default", "my custom flag")

	if ptr == nil {
		t.Fatal("AddCustomFlag returned nil pointer")
	}
	if *ptr != "default" {
		t.Errorf("custom flag default = %q, want \"default\"", *ptr)
	}

	// Verify flag is registered on the cobra command
	f := base.GetCommand().Flags().Lookup("my-flag")
	if f == nil {
		t.Fatal("custom flag not registered on cobra command")
	}
	if f.Shorthand != "m" {
		t.Errorf("flag shorthand = %q, want \"m\"", f.Shorthand)
	}
	if f.DefValue != "default" {
		t.Errorf("flag default = %q, want \"default\"", f.DefValue)
	}
}

func TestAddCustomFlag_NoShorthand(t *testing.T) {
	base := NewBaseCommand("test", "short", "long")
	ptr := base.AddCustomFlag("long-only", "", "val", "long only flag")

	if ptr == nil {
		t.Fatal("AddCustomFlag returned nil")
	}
	if *ptr != "val" {
		t.Errorf("custom flag default = %q, want \"val\"", *ptr)
	}

	f := base.GetCommand().Flags().Lookup("long-only")
	if f == nil {
		t.Fatal("long-only flag not registered")
	}
	if f.Shorthand != "" {
		t.Errorf("expected empty shorthand, got %q", f.Shorthand)
	}
}

// =============================================================================
// SetRunFunc
// =============================================================================

func TestSetRunFunc_SetsRunField(t *testing.T) {
	base := NewBaseCommand("test", "short", "long")

	base.SetRunFunc(func(cfg *CommandConfig, args []string) error {
		return nil
	})

	cmd := base.GetCommand()
	if cmd.Run == nil {
		t.Fatal("SetRunFunc should set cmd.Run")
	}
}



// =============================================================================
// Initialize
// =============================================================================

func TestInitialize(t *testing.T) {
	base := NewBaseCommand("test-init", "init short", "init long")

	err := base.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	cmd := base.GetCommand()
	cfg := base.cfg
	if cfg == nil {
		t.Fatal("cfg should be non-nil after Initialize")
	}
	if cfg.Config == nil {
		t.Fatal("cfg.Config should be non-nil after Initialize")
	}
	if cfg.Logger == nil {
		t.Fatal("cfg.Logger should be non-nil after Initialize")
	}

	// Verify flags are reflected in config
	skipPrompt, _ := cmd.Flags().GetBool("skip-prompt")
	if cfg.SkipPrompt != skipPrompt {
		t.Errorf("cfg.SkipPrompt = %v, want %v", cfg.SkipPrompt, skipPrompt)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if cfg.DryRun != dryRun {
		t.Errorf("cfg.DryRun = %v, want %v", cfg.DryRun, dryRun)
	}
}

func TestInitialize_SkipPrompt(t *testing.T) {
	base := NewBaseCommand("test-init", "init short", "init long")

	// Set skip-prompt flag
	cmd := base.GetCommand()
	cmd.Flags().Set("skip-prompt", "true")

	err := base.Initialize()
	if err != nil {
		t.Fatalf("Initialize with skip-prompt failed: %v", err)
	}
	if !base.cfg.SkipPrompt {
		t.Error("cfg.SkipPrompt should be true")
	}
}

func TestInitialize_DryRun(t *testing.T) {
	base := NewBaseCommand("test-init", "init short", "init long")

	cmd := base.GetCommand()
	cmd.Flags().Set("dry-run", "true")

	err := base.Initialize()
	if err != nil {
		t.Fatalf("Initialize with dry-run failed: %v", err)
	}
	if !base.cfg.DryRun {
		t.Error("cfg.DryRun should be true")
	}
}
