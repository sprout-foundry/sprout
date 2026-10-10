//go:build !js

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent_audit"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"

	core "github.com/sprout-foundry/seed/core"
)

// readRawAuditLines returns the non-empty lines of a JSONL audit file.
func readRawAuditLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestAuditSinkWritesCallEventToLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "shell-audit.jsonl")
	logger, err := tools.NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()

	sink := newAuditSink(logger, nil)
	defer sink.Close()

	sink.EmitCall(agent_audit.CallEvent{
		Provider:         "openai",
		Model:            "gpt-x",
		EndpointHost:     "api.openai.com",
		RequestSHA256:    "abc",
		RequestBytes:     10,
		ResponseSHA256:   "def",
		ResponseBytes:    20,
		PromptTokens:     5,
		CompletionTokens: 6,
		Outcome:          agent_audit.OutcomeOK,
		Trigger:          agent_audit.TriggerUserTurn,
	})

	lines := readRawAuditLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 audit line, got %d", len(lines))
	}
	var ev agent_audit.CallEvent
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("unmarshal call event: %v", err)
	}
	if ev.Provider != "openai" || ev.Model != "gpt-x" {
		t.Errorf("event provider/model = %q/%q", ev.Provider, ev.Model)
	}
	if ev.RequestSHA256 != "abc" || ev.ResponseSHA256 != "def" {
		t.Errorf("event digests = %q/%q", ev.RequestSHA256, ev.ResponseSHA256)
	}
}

// TestAuditSinkViaEmitCallEnriches verifies the full path: agent_audit.EmitCall
// enriches the event (kind, timestamp, context) and the installed sink writes
// the enriched event to the log.
func TestAuditSinkViaEmitCallEnriches(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "shell-audit.jsonl")
	logger, err := tools.NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	sink := installAuditSink(logger, nil)
	defer func() {
		clearAuditSink(sink)
		logger.Close()
	}()

	ctx := agent_audit.WithCallContext(context.Background(), agent_audit.CallContext{ChatID: "c1", SessionID: "s1"})
	agent_audit.EmitCall(ctx, agent_audit.CallEvent{Provider: "openai", Model: "gpt-x", Outcome: agent_audit.OutcomeOK})

	lines := readRawAuditLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 audit line, got %d", len(lines))
	}
	var ev agent_audit.CallEvent
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("unmarshal call event: %v", err)
	}
	if ev.Kind != agent_audit.KindModelCall {
		t.Errorf("kind = %q, want model_call", ev.Kind)
	}
	if ev.ChatID != "c1" || ev.SessionID != "s1" {
		t.Errorf("context not enriched: %+v", ev)
	}
	if ev.Trigger != agent_audit.TriggerUserTurn {
		t.Errorf("trigger = %q, want user_turn", ev.Trigger)
	}
}

func TestAuditSinkWritesToolEventToLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "shell-audit.jsonl")
	logger, err := tools.NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()

	sink := newAuditSink(logger, nil)
	defer sink.Close()

	sink.EmitTool(agent_audit.ToolEvent{
		Tool:         "write_file",
		ArgsSHA256:   "abc",
		ArgsBytes:    12,
		Status:       "ok",
		FilesTouched: []string{"src/a.go"},
	})

	lines := readRawAuditLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 audit line, got %d", len(lines))
	}
	var ev agent_audit.ToolEvent
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("unmarshal tool event: %v", err)
	}
	if ev.Tool != "write_file" || ev.Status != "ok" {
		t.Errorf("tool event = %+v", ev)
	}
	if len(ev.FilesTouched) != 1 || ev.FilesTouched[0] != "src/a.go" {
		t.Errorf("files touched = %v", ev.FilesTouched)
	}
}

func TestAuditSinkEnqueuesToEndpoint(t *testing.T) {
	var mu sync.Mutex
	var batches [][]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &configuration.Config{Audit: &configuration.AuditConfig{Endpoint: server.URL, BatchSize: 2, FlushIntervalSeconds: 1}}
	sink := newAuditSink(nil, cfg)
	defer sink.Close()

	sink.EmitCall(agent_audit.CallEvent{Provider: "p", Outcome: agent_audit.OutcomeOK})
	sink.EmitTool(agent_audit.ToolEvent{Tool: "t", Status: "ok"})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(batches)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) == 0 {
		t.Fatal("expected a batch to be POSTed to the endpoint")
	}
	if len(batches[0]) != 2 {
		t.Errorf("batch size = %d, want 2", len(batches[0]))
	}
}

// TestAuditSinkEndpointRetries verifies a batch is retried after a transient
// failure, without blocking the enqueueing goroutine.
func TestAuditSinkEndpointRetries(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &configuration.Config{Audit: &configuration.AuditConfig{Endpoint: server.URL, BatchSize: 1, FlushIntervalSeconds: 1}}
	sink := newAuditSink(nil, cfg)
	defer sink.Close()

	sink.EmitCall(agent_audit.CallEvent{Provider: "p", Outcome: agent_audit.OutcomeOK})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := attempts
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Fatalf("expected a retry after the first failure, got %d attempts", attempts)
	}
}

func TestAuditSinkNilLoggerIsSafe(t *testing.T) {
	sink := newAuditSink(nil, nil)
	defer sink.Close()
	// Must not panic with a nil logger and no endpoint.
	sink.EmitCall(agent_audit.CallEvent{Provider: "p"})
	sink.EmitTool(agent_audit.ToolEvent{Tool: "t"})
}

// TestAuditForwarderCloseFlushesRemaining verifies Close actually waits for the
// background goroutine to send the queued events before returning.
func TestAuditForwarderCloseFlushesRemaining(t *testing.T) {
	var mu sync.Mutex
	var received int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received += len(batch)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// A long flush interval and a batch size larger than the queued events
	// mean the flush can only happen on Close.
	f := newAuditForwarder(server.URL, 100, 3600)
	f.Enqueue([]byte(`{"kind":"model_call"}`))
	f.Enqueue([]byte(`{"kind":"tool_call"}`))
	f.Close()

	mu.Lock()
	defer mu.Unlock()
	if received != 2 {
		t.Fatalf("expected Close to flush 2 queued events, got %d", received)
	}
}

// TestAuditForwarderCloseIsIdempotent verifies a second Close is a no-op.
func TestAuditForwarderCloseIsIdempotent(t *testing.T) {
	f := newAuditForwarder("http://127.0.0.1:0/nowhere", 1, 3600)
	f.Close()
	f.Close()
}

func TestInstallAndClearAuditSink(t *testing.T) {
	dir := t.TempDir()
	logger, err := tools.NewAuditLogger(filepath.Join(dir, "a.jsonl"))
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()

	owned := installAuditSink(logger, nil)
	if agent_audit.CurrentSink() == nil {
		t.Fatal("expected sink installed")
	}
	clearAuditSink(owned)
	if agent_audit.CurrentSink() != nil {
		t.Error("expected sink cleared")
	}
}

// TestAgentConstructionInstallsAuditSink pins that the production construction
// path installs the per-call audit sink, so provider emissions reach the log.
func TestAgentConstructionInstallsAuditSink(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := newTestAgent(t)
	defer a.Shutdown()

	if agent_audit.CurrentSink() == nil {
		t.Fatal("agent construction should install the audit sink")
	}
}

func TestDeclaredFilePaths(t *testing.T) {
	got := declaredFilePaths(map[string]interface{}{
		"path":  "b.go",
		"files": []interface{}{"a.go", "b.go"},
	})
	want := []string{"a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("declaredFilePaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("declaredFilePaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if declaredFilePaths(nil) != nil {
		t.Error("expected nil for empty args")
	}
}

// TestEmitToolAuditRecordsEvent drives the agent's tool-audit helper and
// asserts the event lands in the local log with a digest and no raw args.
func TestEmitToolAuditRecordsEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "shell-audit.jsonl")
	logger, err := tools.NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	sink := installAuditSink(logger, nil)
	defer func() {
		clearAuditSink(sink)
		logger.Close()
	}()

	a := &Agent{}
	a.emitToolAudit(context.Background(), "write_file", map[string]interface{}{"path": "secret/path.go", "content": "TOP_SECRET_CONTENT"}, "ok")

	lines := readRawAuditLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 audit line, got %d", len(lines))
	}
	if strings.Contains(lines[0], "TOP_SECRET_CONTENT") {
		t.Fatalf("tool audit event leaked raw args: %s", lines[0])
	}
	var ev agent_audit.ToolEvent
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Tool != "write_file" || ev.Status != "ok" {
		t.Errorf("event = %+v", ev)
	}
	if ev.ArgsSHA256 == "" || ev.ArgsBytes == 0 {
		t.Error("args digest/bytes not recorded")
	}
	if len(ev.FilesTouched) != 1 || ev.FilesTouched[0] != "secret/path.go" {
		t.Errorf("files touched = %v", ev.FilesTouched)
	}
}

// TestToolExecutionEmitsAuditEvent drives a real tool through the seed
// registry (the production model-driven tool path) and asserts a tool audit
// event lands in the local log with a digest and no raw arguments.
func TestToolExecutionEmitsAuditEvent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	a := newTestAgent(t)
	defer a.Shutdown()

	registry := NewSeedToolRegistry(a)
	argsJSON := `{"todos":[{"content":"AUDIT_SECRET_TODO_TEXT","status":"pending"}]}`
	registry.Execute(context.Background(), []core.ToolCall{{
		ID:   "audit-tool-1",
		Type: "function",
		Function: core.ToolCallFunction{
			Name:      "todo_write",
			Arguments: argsJSON,
		},
	}})

	logPath := filepath.Join(stateDir, shellAuditFileName)
	lines := readRawAuditLines(t, logPath)

	var found bool
	for _, line := range lines {
		if strings.Contains(line, "AUDIT_SECRET_TODO_TEXT") {
			t.Fatalf("tool audit event leaked raw args: %s", line)
		}
		var ev agent_audit.ToolEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Kind == agent_audit.KindToolCall && ev.Tool == "todo_write" {
			found = true
			if ev.ArgsSHA256 == "" {
				t.Error("tool audit event missing args digest")
			}
			if ev.Status == "" {
				t.Error("tool audit event missing status")
			}
		}
	}
	if !found {
		t.Fatalf("expected a todo_write tool audit event, got lines: %v", lines)
	}
}

// TestAuditTriggerSubagent verifies a subagent's tool event is tagged with the
// subagent trigger.
func TestAuditTriggerSubagent(t *testing.T) {
	a := &Agent{subagentDepth: 1}
	if got := a.auditTrigger(); got != agent_audit.TriggerSubagent {
		t.Errorf("auditTrigger() = %q, want %q", got, agent_audit.TriggerSubagent)
	}
	primary := &Agent{}
	if got := primary.auditTrigger(); got != agent_audit.TriggerUserTurn {
		t.Errorf("auditTrigger() = %q, want %q", got, agent_audit.TriggerUserTurn)
	}
}

// TestClearAuditSinkDoesNotClobberOtherAgents verifies the process-wide sink is
// only cleared by the agent that currently owns it.
func TestClearAuditSinkDoesNotClobberOtherAgents(t *testing.T) {
	dir := t.TempDir()
	l1, err := tools.NewAuditLogger(filepath.Join(dir, "a1.jsonl"))
	if err != nil {
		t.Fatalf("logger 1: %v", err)
	}
	l2, err := tools.NewAuditLogger(filepath.Join(dir, "a2.jsonl"))
	if err != nil {
		t.Fatalf("logger 2: %v", err)
	}
	defer l1.Close()
	defer l2.Close()

	s1 := installAuditSink(l1, nil)
	s2 := installAuditSink(l2, nil)

	// s2 installed last and owns the process-global sink.
	clearAuditSink(s1)
	if currentAuditSink() != s2 {
		t.Error("clearing s1 must not remove s2's sink")
	}
	clearAuditSink(s2)
	if agent_audit.CurrentSink() != nil {
		t.Error("expected sink cleared after owner closes")
	}
}
