package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_audit"
)

// auditRecorder captures emitted call events for assertions.
type auditRecorder struct {
	mu    sync.Mutex
	calls []agent_audit.CallEvent
}

func (r *auditRecorder) EmitCall(ev agent_audit.CallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ev)
}

func (r *auditRecorder) EmitTool(agent_audit.ToolEvent) {}

func (r *auditRecorder) snapshot() []agent_audit.CallEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]agent_audit.CallEvent, len(r.calls))
	copy(out, r.calls)
	return out
}

const auditTestPromptText = "SECRET_PROMPT_TEXT_should_never_appear"

func auditTestConfig(endpoint string) *ProviderConfig {
	return &ProviderConfig{
		Name:     "audit-test",
		Endpoint: endpoint,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "audit-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000},
	}
}

func TestSendChatRequestEmitsOneAuditEvent(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","object":"chat.completion","created":1,"model":"audit-model","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: auditTestPromptText}}, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev.Provider != "audit-test" || ev.Model != "audit-model" {
		t.Errorf("provider/model = %q/%q, want audit-test/audit-model", ev.Provider, ev.Model)
	}
	if ev.Outcome != agent_audit.OutcomeOK {
		t.Errorf("outcome = %q, want ok", ev.Outcome)
	}
	if ev.Trigger != agent_audit.TriggerUserTurn {
		t.Errorf("trigger = %q, want user_turn", ev.Trigger)
	}
	if ev.RequestSHA256 == "" || ev.RequestBytes == 0 {
		t.Error("request digest/bytes not recorded")
	}
	if ev.ResponseSHA256 == "" || ev.ResponseBytes == 0 {
		t.Error("response digest/bytes not recorded")
	}
	if ev.PromptTokens != 11 || ev.CompletionTokens != 7 {
		t.Errorf("tokens = %d/%d, want 11/7", ev.PromptTokens, ev.CompletionTokens)
	}
	if ev.EndpointHost == "" {
		t.Error("endpoint host not recorded")
	}
	if ev.Streaming {
		t.Error("non-streaming call marked streaming")
	}
}

func TestSendChatRequestStreamEmitsOneAuditEvent(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	_, err = provider.SendChatRequestStream(context.Background(), []api.Message{{Role: "user", Content: auditTestPromptText}}, nil, "", false, nil)
	if err != nil {
		t.Fatalf("SendChatRequestStream: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if !ev.Streaming {
		t.Error("streaming call not marked streaming")
	}
	if ev.Outcome != agent_audit.OutcomeOK {
		t.Errorf("outcome = %q, want ok", ev.Outcome)
	}
	if ev.ResponseSHA256 == "" || ev.ResponseBytes == 0 {
		t.Error("streamed response digest/bytes not recorded")
	}
}

// TestAuditEventCarriesNoPromptText is the guardrail: no audit event may
// contain the prompt text, in any field.
func TestAuditEventCarriesNoPromptText(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","model":"audit-model","choices":[{"index":0,"message":{"role":"assistant","content":"` + auditTestPromptText + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: auditTestPromptText}}, nil, "", false); err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	encoded, err := json.Marshal(events[0])
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(encoded), auditTestPromptText) {
		t.Fatalf("audit event leaked prompt text: %s", encoded)
	}
}

// TestAuditEventOnError verifies a failed call still emits exactly one event,
// with outcome=error.
func TestAuditEventOnError(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "x"}}, nil, "", false); err == nil {
		t.Fatal("expected error from 500 response")
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event on error, got %d", len(events))
	}
	if events[0].Outcome != agent_audit.OutcomeError {
		t.Errorf("outcome = %q, want error", events[0].Outcome)
	}
}

// TestAuditEventOnFailover verifies the max_completion_tokens retry path emits
// one event marked as a failover, not two.
func TestAuditEventOnFailover(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	var calls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		attempt := calls
		mu.Unlock()

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, hasMaxCompletion := body["max_completion_tokens"]

		if attempt == 1 || !hasMaxCompletion {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"max_tokens is unsupported; use max_completion_tokens"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","model":"audit-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "x"}}, nil, "", false); err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event across a failover, got %d", len(events))
	}
	if events[0].Outcome != agent_audit.OutcomeFailover {
		t.Errorf("outcome = %q, want failover", events[0].Outcome)
	}
	if !events[0].Failover {
		t.Error("failover flag not set")
	}
}

// TestAuditTriggerToolFollowUp verifies a request whose last message is a tool
// result is recorded as a tool-call follow-up.
func TestAuditTriggerToolFollowUp(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","model":"audit-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	msgs := []api.Message{
		{Role: "user", Content: "do it"},
		{Role: "assistant", Content: ""},
		{Role: "tool", Content: "result"},
	}
	if _, err := provider.SendChatRequest(context.Background(), msgs, nil, "", false); err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Trigger != agent_audit.TriggerToolFollowUp {
		t.Errorf("trigger = %q, want %q", events[0].Trigger, agent_audit.TriggerToolFollowUp)
	}
}

// TestAuditSubagentTrigger verifies the subagent flag on the call context wins.
func TestAuditSubagentTrigger(t *testing.T) {
	rec := &auditRecorder{}
	agent_audit.SetSink(rec)
	defer agent_audit.SetSink(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","model":"audit-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(auditTestConfig(server.URL + "/v1/chat/completions"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	ctx := agent_audit.WithCallContext(context.Background(), agent_audit.CallContext{
		ChatID: "chat-1", SessionID: "sess-1", Subagent: true,
	})
	if _, err := provider.SendChatRequest(ctx, []api.Message{{Role: "user", Content: "x"}}, nil, "", false); err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	events := rec.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Trigger != agent_audit.TriggerSubagent {
		t.Errorf("trigger = %q, want subagent", events[0].Trigger)
	}
	if events[0].ChatID != "chat-1" || events[0].SessionID != "sess-1" {
		t.Errorf("chat/session not carried from ctx: %+v", events[0])
	}
}
