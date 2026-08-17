package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/search"
)

func setupScopedStateTest(t *testing.T) (stateDir, workingDir string) {
	t.Helper()
	stateDir = t.TempDir()
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { getStateDirFunc = orig })

	oldUpdater := search.ResetGlobalUpdaterForTest()
	search.GlobalUpdater = search.NewIndexUpdater(
		filepath.Join(stateDir, "search-index.json"), stateDir)
	t.Cleanup(func() { search.RestoreGlobalUpdater(oldUpdater) })

	workingDir = filepath.Join(stateDir, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}
	return stateDir, workingDir
}

func newScopedStateAgent(messages ...api.Message) *Agent {
	a := &Agent{state: NewAgentStateManager(false)}
	a.state.SetMessages(messages)
	return a
}

func TestWriteFileAtomic_ReplacesContentAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new content"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Fatalf("content = %q, want %q", got, "new content")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected 1 file after atomic write, got %d: %v", len(entries), names)
	}
}

func TestWriteFileAtomic_CreatesMissingDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "state.json")
	if err := writeFileAtomic(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteFileAtomic_FailureLeavesPriorFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	unwritable := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(unwritable, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unwritable, 0o700) })

	if err := writeFileAtomic(filepath.Join(unwritable, "state.json"), []byte("bad"), 0o600); err == nil {
		t.Fatal("expected error writing into unwritable dir")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "good" {
		t.Fatalf("prior file content = %q, want %q", got, "good")
	}
}

func TestSaveStateScoped_BackupPreviousGeneration(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "first"})
	if err := a.SaveStateScoped("gen", workingDir); err != nil {
		t.Fatalf("first save: %v", err)
	}

	a = newScopedStateAgent(
		api.Message{Role: "user", Content: "first"},
		api.Message{Role: "assistant", Content: "second generation"},
	)
	if err := a.SaveStateScoped("gen", workingDir); err != nil {
		t.Fatalf("second save: %v", err)
	}

	stateFile, err := buildScopedSessionFilePath(stateDir, "gen", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	bakData, err := os.ReadFile(stateFile + ".bak")
	if err != nil {
		t.Fatalf("expected .bak to exist after overwrite: %v", err)
	}
	var bakState ConversationState
	if err := json.Unmarshal(bakData, &bakState); err != nil {
		t.Fatalf("unmarshal backup: %v", err)
	}
	if len(bakState.Messages) != 1 || bakState.Messages[0].Content != "first" {
		t.Fatalf("backup should hold first generation, got %d messages", len(bakState.Messages))
	}

	mainData, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var mainState ConversationState
	if err := json.Unmarshal(mainData, &mainState); err != nil {
		t.Fatalf("unmarshal main: %v", err)
	}
	if len(mainState.Messages) != 2 {
		t.Fatalf("main should hold second generation, got %d messages", len(mainState.Messages))
	}
}

func TestSaveStateScoped_FirstSaveHasNoBackup(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "hello"})
	if err := a.SaveStateScoped("fresh", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}

	stateDirResolved, err := GetStateDir()
	if err != nil {
		t.Fatal(err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDirResolved, "fresh", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected no .bak on first save, stat err: %v", err)
	}
}

func TestLoadStateWithoutAgentScoped_FallsBackToBackupOnCorruptMain(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "recover me"})
	if err := a.SaveStateScoped("corrupt", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDir, "corrupt", workingDir)
	if err != nil {
		t.Fatal(err)
	}

	a2 := newScopedStateAgent(api.Message{Role: "user", Content: "second"})
	if err := a2.SaveStateScoped("corrupt", workingDir); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if err := os.WriteFile(stateFile, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadStateWithoutAgentScoped("corrupt", workingDir)
	if err != nil {
		t.Fatalf("expected backup fallback, got: %v", err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "recover me" {
		t.Fatalf("backup fallback returned wrong generation: %+v", loaded.Messages)
	}
}

func TestLoadStateWithoutAgentScoped_NoBackupReturnsUnmarshalError(t *testing.T) {
	stateDir, workingDir := setupScopedStateTest(t)
	stateFile, err := buildScopedSessionFilePath(stateDir, "onlymain", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = LoadStateWithoutAgentScoped("onlymain", workingDir)
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got: %v", err)
	}
}

func TestDeleteSessionScoped_RemovesBackupToo(t *testing.T) {
	_, workingDir := setupScopedStateTest(t)

	a := newScopedStateAgent(api.Message{Role: "user", Content: "bye"})
	if err := a.SaveStateScoped("todelete", workingDir); err != nil {
		t.Fatalf("save: %v", err)
	}
	a2 := newScopedStateAgent(
		api.Message{Role: "user", Content: "bye"},
		api.Message{Role: "assistant", Content: "more"},
	)
	if err := a2.SaveStateScoped("todelete", workingDir); err != nil {
		t.Fatalf("second save: %v", err)
	}

	if err := DeleteSessionScoped("todelete", workingDir); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stateDirResolved, err := GetStateDir()
	if err != nil {
		t.Fatal(err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDirResolved, "todelete", workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("main file still present: %v", err)
	}
	if _, err := os.Stat(stateFile + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup file still present: %v", err)
	}
}
