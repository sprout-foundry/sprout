package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfig_MergesAgainstLatestDiskState(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	m1, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 1: %v", err)
	}
	m2, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}

	cfg1 := m1.GetConfig()
	if cfg1.CustomProviders == nil {
		cfg1.CustomProviders = map[string]CustomProviderConfig{}
	}
	cfg1.CustomProviders["ai-worker"] = CustomProviderConfig{
		Name:            "ai-worker",
		Endpoint:        "https://example.test/v1/chat/completions",
		ModelName:       "openai/gpt-oss-20b",
		ContextSize:     120000,
		ReasoningEffort: "low",
		RequiresAPIKey:  true,
	}
	if err := m1.SaveConfig(); err != nil {
		t.Fatalf("save manager 1 config: %v", err)
	}

	// Manager 2 is stale (loaded before manager 1 save); apply an unrelated write.
	cfg2 := m2.GetConfig()
	projectPath := filepath.Join(homeDir, "project")
	cfg2.CommandHistoryByPath = map[string][]string{
		projectPath: {"hello"},
	}
	if err := m2.SaveConfig(); err != nil {
		t.Fatalf("save manager 2 config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("reload merged config: %v", err)
	}
	cp, ok := loaded.CustomProviders["ai-worker"]
	if !ok {
		t.Fatalf("expected ai-worker custom provider to remain after stale save")
	}
	if cp.ReasoningEffort != "low" {
		t.Fatalf("expected reasoning_effort low, got %q", cp.ReasoningEffort)
	}
	if got := loaded.CommandHistoryByPath[projectPath]; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("expected command history update to persist, got %#v", got)
	}
}

func TestSaveConfig_AppliesDeletionAndScalarUpdates(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	m1, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 1: %v", err)
	}
	m2, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}

	cfg1 := m1.GetConfig()
	cfg1.ProviderPriority = []string{"openrouter", "deepinfra"}
	cfg1.ResourceDirectory = "resources-a"
	if err := m1.SaveConfig(); err != nil {
		t.Fatalf("save manager 1 config: %v", err)
	}

	// Stale manager updates one scalar and clears provider priority (deletion/change).
	cfg2 := m2.GetConfig()
	cfg2.ResourceDirectory = "resources-b"
	cfg2.ProviderPriority = nil
	if err := m2.SaveConfig(); err != nil {
		t.Fatalf("save manager 2 config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("reload merged config: %v", err)
	}
	if loaded.ResourceDirectory != "resources-b" {
		t.Fatalf("expected latest scalar from manager2, got %q", loaded.ResourceDirectory)
	}
	if len(loaded.ProviderPriority) != 0 {
		t.Fatalf("expected provider_priority to be cleared, got %#v", loaded.ProviderPriority)
	}
}

func TestSaveConfig_WritesConfigFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	m, err := NewManager()
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	m.GetConfig().ResourceDirectory = "resources"
	if err := m.SaveConfig(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	configPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("get config path: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to exist after save: %v", err)
	}
}
