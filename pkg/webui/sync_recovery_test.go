//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"os"
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

// newNoProviderConfig sets up a temp config directory with LastUsedProvider
// set to "editor" so that isProviderAvailable() returns false, causing
// getClientAgent to return ErrNoProviderConfigured. Returns the config dir.
func newNoProviderConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a minimal config.json with last_used_provider = "editor"
	// (snake_case to match the JSON tag on Config.LastUsedProvider)
	cfgPath := filepath.Join(tmpDir, configuration.ConfigFileName)
	if err := os.WriteFile(cfgPath, []byte(`{"last_used_provider":"editor"}`), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	return tmpDir
}

// ---------------------------------------------------------------------------
// Container Death Recovery Tests
// ---------------------------------------------------------------------------

func TestHandleContainerRecoveryWithSeqs_NoAgent(t *testing.T) {
	server := newTestHeartbeatServer(t)
	newNoProviderConfig(t)

	_, err := server.HandleContainerRecoveryWithSeqs(context.Background(), "client-1", map[string]int64{
		"foo.txt": 5,
	})
	if err == nil {
		t.Fatal("expected error when no provider is configured, got nil")
	}
	if !strings.Contains(err.Error(), "no agent found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleContainerRecovery_NoAgent(t *testing.T) {
	server := newTestHeartbeatServer(t)
	newNoProviderConfig(t)

	_, err := server.HandleContainerRecovery(context.Background(), "client-1", 5)
	if err == nil {
		t.Fatal("expected error when no provider is configured, got nil")
	}
	if !strings.Contains(err.Error(), "no agent found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleContainerRecoveryWithSeqs_Success(t *testing.T) {
	server := newTestHeartbeatServer(t)

	ag := newSyncTestAgent(t)
	ag.SetFileMetadata("foo.txt", agent.WorkspaceFileMetadata{
		BrowserSeq:          3,
		ContainerSeq:        5,
		LastSyncedBrowser:   3,
		LastSyncedContainer: 3,
	})
	ag.SetFileMetadata("bar.txt", agent.WorkspaceFileMetadata{
		BrowserSeq:          2,
		ContainerSeq:        2,
		LastSyncedBrowser:   2,
		LastSyncedContainer: 2,
	})
	// Register agent in client context so getClientAgent can find it
	server.getOrCreateClientContext("client-1").Agent = ag

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
	// Register agent in client context so getClientAgent can find it
	server.getOrCreateClientContext("client-1").Agent = ag

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
	newNoProviderConfig(t)
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
		BrowserSeq:          5,
		ContainerSeq:        5,
		LastSyncedBrowser:   5,
		LastSyncedContainer: 5,
	})
	// Register agent in client context so getClientAgent can find it
	server.getOrCreateClientContext("client-1").Agent = ag

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
