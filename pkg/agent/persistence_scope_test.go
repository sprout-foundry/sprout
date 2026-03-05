package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestLoadStateWithoutAgentScoped_ResolvesByWorkingDirectory(t *testing.T) {
	stateDir := t.TempDir()
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { getStateDirFunc = orig })

	wd1 := filepath.Join(stateDir, "w1")
	wd2 := filepath.Join(stateDir, "w2")
	if err := os.MkdirAll(wd1, 0o755); err != nil {
		t.Fatalf("mkdir wd1: %v", err)
	}
	if err := os.MkdirAll(wd2, 0o755); err != nil {
		t.Fatalf("mkdir wd2: %v", err)
	}

	writeSession := func(workingDir, name string) {
		path, err := buildScopedSessionFilePath(stateDir, "workflow", workingDir)
		if err != nil {
			t.Fatalf("build path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir scope: %v", err)
		}
		payload, err := json.MarshalIndent(ConversationState{
			SessionID:        "workflow",
			Name:             name,
			WorkingDirectory: workingDir,
			LastUpdated:      time.Now(),
		}, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeSession(wd1, "workflow-w1")
	writeSession(wd2, "workflow-w2")

	got, err := LoadStateWithoutAgentScoped("workflow", wd1)
	if err != nil {
		t.Fatalf("LoadStateWithoutAgentScoped(wd1): %v", err)
	}
	if got.WorkingDirectory != wd1 {
		t.Fatalf("unexpected working directory: %q", got.WorkingDirectory)
	}

	got, err = LoadStateWithoutAgentScoped("workflow", wd2)
	if err != nil {
		t.Fatalf("LoadStateWithoutAgentScoped(wd2): %v", err)
	}
	if got.WorkingDirectory != wd2 {
		t.Fatalf("unexpected working directory: %q", got.WorkingDirectory)
	}

	unknownDir := filepath.Join(stateDir, "unknown")
	if err := os.MkdirAll(unknownDir, 0o755); err != nil {
		t.Fatalf("mkdir unknown: %v", err)
	}
	if _, err := LoadStateWithoutAgentScoped("workflow", unknownDir); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}
}

func TestSaveStateScoped_WritesScopedPath(t *testing.T) {
	stateDir := t.TempDir()
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { getStateDirFunc = orig })

	workingDir := filepath.Join(stateDir, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}

	a := &Agent{
		messages: []api.Message{{Role: "user", Content: "hello"}},
	}
	if err := a.SaveStateScoped("workflow", workingDir); err != nil {
		t.Fatalf("SaveStateScoped: %v", err)
	}

	path, err := buildScopedSessionFilePath(stateDir, "workflow", workingDir)
	if err != nil {
		t.Fatalf("build path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scoped state file: %v", err)
	}
	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.SessionID != "workflow" {
		t.Fatalf("unexpected session id: %q", state.SessionID)
	}
	if state.WorkingDirectory != workingDir {
		t.Fatalf("unexpected working directory: %q", state.WorkingDirectory)
	}
}
