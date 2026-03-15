package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(original)
	})
}

func newHistoryTestAgent(t *testing.T, workDir string) *Agent {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	withWorkingDir(t, workDir)

	manager, err := configuration.NewManager()
	if err != nil {
		t.Fatalf("failed to initialize config manager: %v", err)
	}

	return &Agent{configManager: manager}
}

func TestLoadHistoryFromConfig_UsesPathScopedHistory(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project-a")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}

	agent := newHistoryTestAgent(t, workDir)
	pathKey := agent.historyPathKey()
	if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.CommandHistoryByPath = map[string][]string{
			pathKey: {"status", "help"},
		}
		cfg.HistoryIndexByPath = map[string]int{
			pathKey: 1,
		}
		cfg.CommandHistory = []string{"legacy-should-not-win"}
		cfg.HistoryIndex = 5
		return nil
	}); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	agent.loadHistoryFromConfig()

	if len(agent.commandHistory) != 2 || agent.commandHistory[0] != "status" || agent.commandHistory[1] != "help" {
		t.Fatalf("expected path-scoped command history, got %#v", agent.commandHistory)
	}
	if agent.historyIndex != -1 {
		t.Fatalf("expected history index reset to -1, got %d", agent.historyIndex)
	}
}

func TestLoadHistoryFromConfig_FallsBackToLegacyGlobalHistory(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project-b")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}

	agent := newHistoryTestAgent(t, workDir)
	if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.CommandHistoryByPath = map[string][]string{}
		cfg.HistoryIndexByPath = map[string]int{}
		cfg.CommandHistory = []string{"legacy-1", "legacy-2"}
		cfg.HistoryIndex = 1
		return nil
	}); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	agent.loadHistoryFromConfig()

	if len(agent.commandHistory) != 2 || agent.commandHistory[0] != "legacy-1" || agent.commandHistory[1] != "legacy-2" {
		t.Fatalf("expected legacy command history fallback, got %#v", agent.commandHistory)
	}
	if agent.historyIndex != -1 {
		t.Fatalf("expected history index reset to -1, got %d", agent.historyIndex)
	}
}

func TestSaveHistoryToConfig_WritesPathScopedHistory(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project-c")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}

	agent := newHistoryTestAgent(t, workDir)
	agent.commandHistory = []string{"cmd-a", "cmd-b"}
	agent.historyIndex = 0

	agent.saveHistoryToConfig()

	cfg := agent.configManager.GetConfig()
	pathKey := agent.historyPathKey()
	history, ok := cfg.CommandHistoryByPath[pathKey]
	if !ok {
		t.Fatalf("expected path-scoped history entry for %s", pathKey)
	}
	if len(history) != 2 || history[0] != "cmd-a" || history[1] != "cmd-b" {
		t.Fatalf("unexpected saved history: %#v", history)
	}
	if idx, ok := cfg.HistoryIndexByPath[pathKey]; !ok || idx != 0 {
		t.Fatalf("unexpected saved history index: exists=%v value=%d", ok, idx)
	}
	if len(cfg.CommandHistory) != 0 || cfg.HistoryIndex != 0 {
		t.Fatalf("expected legacy history fields to be cleared, got CommandHistory=%#v HistoryIndex=%d", cfg.CommandHistory, cfg.HistoryIndex)
	}
}
