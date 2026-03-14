package agent

import (
	"encoding/json"
	"fmt"
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

func TestListSessionsWithTimestampsScoped_OnlyReturnsCurrentDirectorySessions(t *testing.T) {
	stateDir := t.TempDir()
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { getStateDirFunc = orig })

	wd1 := filepath.Join(stateDir, "project-a")
	wd2 := filepath.Join(stateDir, "project-b")
	for _, wd := range []string{wd1, wd2} {
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", wd, err)
		}
	}

	writeScopedSession := func(workingDir, sessionID string, updated time.Time) {
		path, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
		if err != nil {
			t.Fatalf("build path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir scope: %v", err)
		}
		payload, err := json.MarshalIndent(ConversationState{
			SessionID:        sessionID,
			WorkingDirectory: workingDir,
			LastUpdated:      updated,
		}, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeScopedSession(wd1, "a-older", time.Now().Add(-2*time.Hour))
	writeScopedSession(wd1, "a-newer", time.Now().Add(-1*time.Hour))
	writeScopedSession(wd2, "b-only", time.Now())

	sessions, err := ListSessionsWithTimestampsScoped(wd1)
	if err != nil {
		t.Fatalf("ListSessionsWithTimestampsScoped: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for wd1, got %d", len(sessions))
	}
	if sessions[0].SessionID != "a-newer" || sessions[1].SessionID != "a-older" {
		t.Fatalf("unexpected session order/content: %#v", sessions)
	}
}

func TestCleanupMemorySessions_PrunesOnlyCurrentDirectoryScope(t *testing.T) {
	stateDir := t.TempDir()
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { getStateDirFunc = orig })

	wd1 := filepath.Join(stateDir, "project-a")
	wd2 := filepath.Join(stateDir, "project-b")
	for _, wd := range []string{wd1, wd2} {
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", wd, err)
		}
	}

	writeScopedSession := func(workingDir, sessionID string, updated time.Time) {
		path, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
		if err != nil {
			t.Fatalf("build path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir scope: %v", err)
		}
		payload, err := json.MarshalIndent(ConversationState{
			SessionID:        sessionID,
			WorkingDirectory: workingDir,
			LastUpdated:      updated,
		}, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < sessionRetentionLimit+2; i++ {
		writeScopedSession(wd1, fmt.Sprintf("a-%02d", i), base.Add(time.Duration(i)*time.Minute))
	}
	for i := 0; i < 3; i++ {
		writeScopedSession(wd2, fmt.Sprintf("b-%02d", i), base.Add(time.Duration(i)*time.Minute))
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(wd1); err != nil {
		t.Fatalf("Chdir wd1: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := cleanupMemorySessions(); err != nil {
		t.Fatalf("cleanupMemorySessions: %v", err)
	}

	wd1Sessions, err := ListSessionsWithTimestampsScoped(wd1)
	if err != nil {
		t.Fatalf("list wd1 sessions: %v", err)
	}
	if len(wd1Sessions) != sessionRetentionLimit {
		t.Fatalf("expected %d retained sessions for wd1, got %d", sessionRetentionLimit, len(wd1Sessions))
	}

	wd2Sessions, err := ListSessionsWithTimestampsScoped(wd2)
	if err != nil {
		t.Fatalf("list wd2 sessions: %v", err)
	}
	if len(wd2Sessions) != 3 {
		t.Fatalf("expected wd2 sessions to remain untouched, got %d", len(wd2Sessions))
	}
}
