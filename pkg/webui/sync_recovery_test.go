//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newSyncTestAgent creates a minimal agent for sync recovery tests.
// Backed by a temp config dir to isolate from real user config.
func newSyncTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	ag, err := agent.NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	// Use the public API to replace the config manager.
	// Since configManager is unexported, we use the agent's exported methods.
	// The agent itself is usable for SetFileMetadata/GetFileMetadata/etc.
	_ = mgr // already set via env vars for any code that reads env directly
	return ag
}

// syncRecoveryRunGit runs a git command in the specified directory.
// Prefixed to avoid collision with runGit in git_api_test.go.
func syncRecoveryRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

// ---------------------------------------------------------------------------
// Container Death Recovery Tests
// ---------------------------------------------------------------------------

func TestHandleContainerRecoveryWithSeqs_NoAgent(t *testing.T) {
	server := newTestHeartbeatServer(t)

	_, err := server.HandleContainerRecoveryWithSeqs(context.Background(), "client-1", map[string]int64{
		"foo.txt": 5,
	})
	if err == nil {
		t.Fatal("expected error when agent is nil, got nil")
	}
	if !strings.Contains(err.Error(), "no agent found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleContainerRecovery_NoAgent(t *testing.T) {
	server := newTestHeartbeatServer(t)

	_, err := server.HandleContainerRecovery(context.Background(), "client-1", 5)
	if err == nil {
		t.Fatal("expected error when agent is nil, got nil")
	}
	if !strings.Contains(err.Error(), "no agent found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleContainerRecoveryWithSeqs_Success(t *testing.T) {
	server := newTestHeartbeatServer(t)

	ag := newSyncTestAgent(t)
	ag.SetFileMetadata("foo.txt", agent.WorkspaceFileMetadata{
		BrowserSeq:        3,
		ContainerSeq:      5,
		LastSyncedBrowser: 3,
		LastSyncedContainer: 3,
	})
	ag.SetFileMetadata("bar.txt", agent.WorkspaceFileMetadata{
		BrowserSeq:        2,
		ContainerSeq:      2,
		LastSyncedBrowser: 2,
		LastSyncedContainer: 2,
	})
	server.agent = ag

	browserSeqs := map[string]int64{
		"foo.txt": 3, // container is ahead
		"bar.txt": 2, // in sync
	}

	result, err := server.HandleContainerRecoveryWithSeqs(context.Background(), "client-1", browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.ClientID != "client-1" {
		t.Errorf("expected client_id=client-1, got %s", result.ClientID)
	}
	if len(result.Plan) != 2 {
		t.Fatalf("expected 2 plan entries, got %d", len(result.Plan))
	}

	for _, action := range result.Plan {
		switch action.FilePath {
		case "foo.txt":
			if action.Action != "container_ahead" {
				t.Errorf("foo.txt: expected container_ahead, got %s", action.Action)
			}
			if action.ContainerSeq != 5 {
				t.Errorf("foo.txt: expected container_seq=5, got %d", action.ContainerSeq)
			}
			if action.BrowserSeq != 3 {
				t.Errorf("foo.txt: expected browser_seq=3, got %d", action.BrowserSeq)
			}
		case "bar.txt":
			if action.Action != "sync_ok" {
				t.Errorf("bar.txt: expected sync_ok, got %s", action.Action)
			}
		}
	}
}

func TestHandleContainerRecovery_Fallback(t *testing.T) {
	server := newTestHeartbeatServer(t)
	ag := newSyncTestAgent(t)
	server.agent = ag

	result, err := server.HandleContainerRecovery(context.Background(), "client-1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.ClientID != "client-1" {
		t.Errorf("expected client_id=client-1, got %s", result.ClientID)
	}
	if result.Plan == nil {
		t.Error("expected empty plan slice, got nil")
	}
}

// ---------------------------------------------------------------------------
// makePlanFromResults Tests
// ---------------------------------------------------------------------------

func TestMakePlanFromResults_Empty(t *testing.T) {
	results := []agent.ReconciliationActionResult{}
	plan := makePlanFromResults(results)
	if plan == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(plan) != 0 {
		t.Errorf("expected 0 entries, got %d", len(plan))
	}
}

func TestMakePlanFromResults_Success(t *testing.T) {
	results := []agent.ReconciliationActionResult{
		{
			FilePath:     "a.txt",
			Action:       agent.ReconcileContainerAhead,
			ContainerSeq: 10,
			BrowserSeq:   5,
		},
		{
			FilePath:     "b.txt",
			Action:       agent.ReconcileBrowserAhead,
			ContainerSeq: 3,
			BrowserSeq:   8,
		},
	}

	plan := makePlanFromResults(results)
	if len(plan) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan))
	}

	if plan[0].FilePath != "a.txt" || plan[0].Action != "container_ahead" {
		t.Errorf("plan[0]: %+v", plan[0])
	}
	if plan[1].FilePath != "b.txt" || plan[1].Action != "browser_ahead" {
		t.Errorf("plan[1]: %+v", plan[1])
	}
}

// ---------------------------------------------------------------------------
// Volume Corruption Recovery Tests
// ---------------------------------------------------------------------------

func TestHandleVolumeCorruption_NoGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()
	err := HandleVolumeCorruption(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error when no git dir exists, got nil")
	}
	if !strings.Contains(err.Error(), "OPFS replay") {
		t.Errorf("expected error mentioning OPFS replay, got: %v", err)
	}
}

func TestGitRestoreWorkspace_Success(t *testing.T) {
	tmpDir := t.TempDir()
	syncRecoveryRunGit(t, tmpDir, "init", "-b", "main")
	syncRecoveryRunGit(t, tmpDir, "config", "user.email", "test@test.com")
	syncRecoveryRunGit(t, tmpDir, "config", "user.name", "Test")

	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	syncRecoveryRunGit(t, tmpDir, "add", "test.txt")
	syncRecoveryRunGit(t, tmpDir, "commit", "-m", "initial")

	if err := os.WriteFile(filePath, []byte("modified"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	err := gitRestoreWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("gitRestoreWorkspace failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("file not restored: got %q, want %q", string(content), "original")
	}
}

func TestGitRestoreWorkspace_FailedCheckout(t *testing.T) {
	tmpDir := t.TempDir()
	// Not a git repo
	err := gitRestoreWorkspace(tmpDir)
	if err == nil {
		t.Fatal("expected error when not a git repo, got nil")
	}
	if !strings.Contains(err.Error(), "git checkout failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetGitRemoteURL_Success(t *testing.T) {
	tmpDir := t.TempDir()
	syncRecoveryRunGit(t, tmpDir, "init", "-b", "main")
	syncRecoveryRunGit(t, tmpDir, "config", "user.email", "test@test.com")
	syncRecoveryRunGit(t, tmpDir, "config", "user.name", "Test")
	syncRecoveryRunGit(t, tmpDir, "remote", "add", "origin", "https://github.com/test/repo.git")

	url, err := getGitRemoteURL(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/test/repo.git" {
		t.Errorf("unexpected URL: got %q, want %q", url, "https://github.com/test/repo.git")
	}
}

func TestGetGitRemoteURL_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	syncRecoveryRunGit(t, tmpDir, "init", "-b", "main")

	url, err := getGitRemoteURL(tmpDir)
	if err == nil {
		t.Fatal("expected error when no remote exists, got nil")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestGetGitRemoteURL_NotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	url, err := getGitRemoteURL(tmpDir)
	if err == nil {
		t.Fatal("expected error when not a git repo, got nil")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestGitCloneWorkspace_Success(t *testing.T) {
	srcDir := t.TempDir()
	syncRecoveryRunGit(t, srcDir, "init", "-b", "main")
	syncRecoveryRunGit(t, srcDir, "config", "user.email", "test@test.com")
	syncRecoveryRunGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	syncRecoveryRunGit(t, srcDir, "add", "file.txt")
	syncRecoveryRunGit(t, srcDir, "commit", "-m", "initial")

	parentDir := t.TempDir()
	corruptDir := filepath.Join(parentDir, "clone-target")
	if err := os.MkdirAll(corruptDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "corrupt.txt"), []byte("bad data"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	ctx := context.Background()
	err := gitCloneWorkspace(ctx, corruptDir, srcDir)
	if err != nil {
		t.Fatalf("gitCloneWorkspace failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(corruptDir, "corrupt.txt")); !os.IsNotExist(err) {
		t.Error("corrupt.txt should have been replaced by clone")
	}
	content, err := os.ReadFile(filepath.Join(corruptDir, "file.txt"))
	if err != nil {
		t.Fatalf("read file.txt failed: %v", err)
	}
	if string(content) != "content" {
		t.Errorf("unexpected content: got %q, want %q", string(content), "content")
	}

	backups, _ := filepath.Glob(filepath.Join(parentDir, "clone-target.corrupted.*"))
	if len(backups) > 0 {
		t.Errorf("backup directory should have been removed, found: %v", backups)
	}
}

func TestGitCloneWorkspace_CloneFails_RestoresBackup(t *testing.T) {
	parentDir := t.TempDir()
	corruptDir := filepath.Join(parentDir, "clone-target")
	if err := os.MkdirAll(corruptDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "corrupt.txt"), []byte("bad data"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	ctx := context.Background()
	err := gitCloneWorkspace(ctx, corruptDir, "/nonexistent/source")
	if err == nil {
		t.Fatal("expected error when cloning from non-existent source, got nil")
	}

	if _, err := os.Stat(corruptDir); os.IsNotExist(err) {
		t.Error("original directory should have been restored from backup")
	}
	content, _ := os.ReadFile(filepath.Join(corruptDir, "corrupt.txt"))
	if string(content) != "bad data" {
		t.Errorf("original content not restored: got %q", string(content))
	}
}

// ---------------------------------------------------------------------------
// Send Function Tests (use real WS connections via newTestingConnPair)
// ---------------------------------------------------------------------------

func TestSendSyncReplayFile(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	err := server.SendSyncReplayFile(pair.server, "client-1", "test.txt", "hello world", 42)
	if err != nil {
		t.Fatalf("SendSyncReplayFile failed: %v", err)
	}

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg["type"] != "sync_replay_file" {
		t.Errorf("expected type=sync_replay_file, got %v", msg["type"])
	}
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["client_id"] != "client-1" {
		t.Errorf("expected client_id=client-1, got %v", data["client_id"])
	}
	if data["file_path"] != "test.txt" {
		t.Errorf("expected file_path=test.txt, got %v", data["file_path"])
	}
	if data["content"] != "hello world" {
		t.Errorf("expected content='hello world', got %v", data["content"])
	}
	if int(data["seq"].(float64)) != 42 {
		t.Errorf("expected seq=42, got %v", data["seq"])
	}
}

func TestSendSyncReplayStart(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	err := server.SendSyncReplayStart(pair.server, "client-1", 5)
	if err != nil {
		t.Fatalf("SendSyncReplayStart failed: %v", err)
	}

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg["type"] != "sync_replay_start" {
		t.Errorf("expected type=sync_replay_start, got %v", msg["type"])
	}
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["client_id"] != "client-1" {
		t.Errorf("expected client_id=client-1, got %v", data["client_id"])
	}
	if int(data["file_count"].(float64)) != 5 {
		t.Errorf("expected file_count=5, got %v", data["file_count"])
	}
}

func TestSendSyncReplayComplete(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	err := server.SendSyncReplayComplete(pair.server, "client-1")
	if err != nil {
		t.Fatalf("SendSyncReplayComplete failed: %v", err)
	}

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg["type"] != "sync_replay_complete" {
		t.Errorf("expected type=sync_replay_complete, got %v", msg["type"])
	}
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["client_id"] != "client-1" {
		t.Errorf("expected client_id=client-1, got %v", data["client_id"])
	}
}

func TestSendSyncReconcile(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	reconcileData := &SyncReconcileData{
		ClientID: "client-1",
		Plan: []ReconciliationAction{
			{
				FilePath:     "a.txt",
				Action:       "container_ahead",
				ContainerSeq: 10,
				BrowserSeq:   5,
			},
			{
				FilePath:     "b.txt",
				Action:       "sync_ok",
				ContainerSeq: 3,
				BrowserSeq:   3,
			},
		},
	}

	err := server.SendSyncReconcile(pair.server, reconcileData)
	if err != nil {
		t.Fatalf("SendSyncReconcile failed: %v", err)
	}

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg["type"] != "sync_reconcile" {
		t.Errorf("expected type=sync_reconcile, got %v", msg["type"])
	}
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["client_id"] != "client-1" {
		t.Errorf("expected client_id=client-1, got %v", data["client_id"])
	}
	plan, ok := data["plan"].([]interface{})
	if !ok {
		t.Fatal("plan is not an array")
	}
	if len(plan) != 2 {
		t.Fatalf("expected 2 plan entries, got %d", len(plan))
	}
}

// ---------------------------------------------------------------------------
// handleSyncRecoverMessage Tests
// ---------------------------------------------------------------------------

func TestHandleSyncRecoverMessage_EmptyData(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`{}`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read error response failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "error" {
		t.Errorf("expected type=error, got %v", resp["type"])
	}
}

func TestHandleSyncRecoverMessage_InvalidJSON(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`not valid json`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read error response failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "error" {
		t.Errorf("expected type=error, got %v", resp["type"])
	}
}

func TestHandleSyncRecoverMessage_MissingSeqs(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`{"client_id": "client-1"}`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read error response failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "error" {
		t.Errorf("expected type=error, got %v", resp["type"])
	}
}

func TestHandleSyncRecoverMessage_SeqsNotMap(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`{"seqs": "not a map"}`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read error response failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "error" {
		t.Errorf("expected type=error, got %v", resp["type"])
	}
}

func TestHandleSyncRecoverMessage_Success_NoAgent(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`{"seqs": {"file.txt": 5}}`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read error response failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "error" {
		t.Errorf("expected type=error, got %v", resp["type"])
	}
}

func TestHandleSyncRecoverMessage_Success_SyncOK(t *testing.T) {
	server := newTestHeartbeatServer(t)
	pair := newTestingConnPair(t)

	ag := newSyncTestAgent(t)
	ag.SetFileMetadata("in_sync.txt", agent.WorkspaceFileMetadata{
		BrowserSeq:        5,
		ContainerSeq:      5,
		LastSyncedBrowser: 5,
		LastSyncedContainer: 5,
	})
	server.agent = ag

	msg := &WebSocketMessage{
		Type: AllowedMessageTypeSyncRecover,
		Data: json.RawMessage(`{"seqs": {"in_sync.txt": 5}}`),
	}

	server.handleSyncRecoverMessage(pair.server, "test-session", msg, "client-1")

	pair.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["type"] != "sync_reconcile" {
		t.Errorf("expected sync_reconcile, got %v", resp["type"])
	}
	data := resp["data"].(map[string]interface{})
	plan := data["plan"].([]interface{})
	if len(plan) != 1 {
		t.Fatalf("expected 1 plan entry, got %d", len(plan))
	}
	planEntry := plan[0].(map[string]interface{})
	if planEntry["action"] != "sync_ok" {
		t.Errorf("expected action=sync_ok, got %v", planEntry["action"])
	}
}

// ---------------------------------------------------------------------------
// Outbound Registry Tests
// ---------------------------------------------------------------------------

func TestSyncRecoveryOutboundTypesRegistered(t *testing.T) {
	requiredTypes := []string{
		"sync_reconcile",
		"sync_replay_start",
		"sync_replay_file",
		"sync_replay_complete",
	}
	for _, msgType := range requiredTypes {
		if !validateOutboundMessageType(msgType) {
			t.Errorf("outbound type %q should be registered but validateOutboundMessageType returned false", msgType)
		}
	}
}

func TestSyncRecoverMessageInAllowedTypes(t *testing.T) {
	if !allowedMessageTypes[AllowedMessageTypeSyncRecover] {
		t.Error("sync_recover should be in allowedMessageTypes")
	}
	if AllowedMessageTypeSyncRecover != "sync_recover" {
		t.Errorf("AllowedMessageTypeSyncRecover = %q, want %q", AllowedMessageTypeSyncRecover, "sync_recover")
	}
}
