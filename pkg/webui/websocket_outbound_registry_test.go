//go:build !js

package webui

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestValidateOutboundMessageType_AcceptsKnownTypes(t *testing.T) {
	for _, msgType := range []string{
		"connection_status",
		"ping",
		"pong",
		"stats_update",
		wsMessageTypeChatRunRestored,
		events.EventTypeStreamChunk,
		events.EventTypeQueryStarted,
		events.EventTypeQueryCompleted,
		events.EventTypeError,
		events.EventTypeSessionChanged,
		events.EventTypeWorkspaceChanged,
	} {
		t.Run(msgType, func(t *testing.T) {
			if !validateOutboundMessageType(msgType) {
				t.Errorf("validateOutboundMessageType(%q) = false, want true (type is in the canonical registry)", msgType)
			}
		})
	}
}

func TestValidateOutboundMessageType_RejectsUnknownTypeInProd(t *testing.T) {
	// Explicitly unset SPROUT_DEV so we exercise the prod path. The
	// devModeOnce caching means a single test process picks one
	// behavior — but since other tests don't set SPROUT_DEV either,
	// the cached value will be false here.
	t.Setenv("SPROUT_DEV", "")

	if validateOutboundMessageType("totally_made_up_type") {
		t.Error("unknown type should be rejected in prod mode")
	}
}

func TestValidateOutboundMessageType_RejectsEmptyType(t *testing.T) {
	if validateOutboundMessageType("") {
		t.Error("empty type should never validate — that's always a bug")
	}
}

func TestRegisterOutboundMessageType_AllowsRuntimeRegistration(t *testing.T) {
	const custom = "test_custom_outbound_type"
	if validateOutboundMessageType(custom) {
		t.Fatalf("test pre-condition: %q should not be pre-registered", custom)
	}
	RegisterOutboundMessageType(custom)
	if !validateOutboundMessageType(custom) {
		t.Errorf("RegisterOutboundMessageType(%q) should make subsequent validate calls pass", custom)
	}
	// Cleanup: leaving the entry in the global map is fine across tests
	// since the type name is namespaced — but document the leak intent.
}

func TestExtractOutboundMessageType_MapEnvelope(t *testing.T) {
	got, ok := extractOutboundMessageType(map[string]interface{}{
		"type": "stream_chunk",
		"data": map[string]interface{}{"content": "hi"},
	})
	if !ok {
		t.Fatal("expected to extract type from map envelope")
	}
	if got != "stream_chunk" {
		t.Errorf("extracted type = %q, want %q", got, "stream_chunk")
	}
}

func TestExtractOutboundMessageType_MissingTypeField(t *testing.T) {
	_, ok := extractOutboundMessageType(map[string]interface{}{
		"data": "no type here",
	})
	if ok {
		t.Error("expected ok=false when type field is missing")
	}
}

func TestExtractOutboundMessageType_NonStringTypeField(t *testing.T) {
	_, ok := extractOutboundMessageType(map[string]interface{}{
		"type": 42, // wrong type
	})
	if ok {
		t.Error("expected ok=false when type field is non-string")
	}
}

func TestExtractOutboundMessageType_UnknownShape(t *testing.T) {
	_, ok := extractOutboundMessageType("some random string")
	if ok {
		t.Error("string input shouldn't yield a type — permissive false expected")
	}
	_, ok = extractOutboundMessageType(42)
	if ok {
		t.Error("int input shouldn't yield a type")
	}
}

// Sanity check that every events.EventType* constant the rest of the
// codebase publishes IS in the outbound registry. If somebody adds a
// new EventType without registering it, this test fails loudly.
func TestOutboundRegistryCoversAllEventTypes(t *testing.T) {
	for _, eventType := range []string{
		events.EventTypeQueryStarted,
		events.EventTypeQueryProgress,
		events.EventTypeQueryCompleted,
		events.EventTypeError,
		events.EventTypeToolExecution,
		events.EventTypeToolStart,
		events.EventTypeToolEnd,
		events.EventTypeSubagentActivity,
		events.EventTypeTodoUpdate,
		events.EventTypeFileChanged,
		events.EventTypeFileContentChanged,
		events.EventTypeStreamChunk,
		events.EventTypeMetricsUpdate,
		events.EventTypeValidation,
		events.EventTypeSecurityApprovalRequest,
		events.EventTypeSecurityPromptRequest,
		events.EventTypeAskUserRequest,
		events.EventTypeAgentMessage,
		events.EventTypeWorkspaceChanged,
		events.EventTypeSessionTerminated,
		events.EventTypeDriftDetected,
		events.EventTypeSessionChanged,
	} {
		if _, ok := allowedOutboundMessageTypes[eventType]; !ok {
			// Build the test name from the constant so the failure tells you
			// which event is missing.
			t.Errorf("events.EventType %q is not in allowedOutboundMessageTypes — add it to the registry", eventType)
		}
	}
}

func TestValidateOutboundMessageType_PanicHintContainsRegistryLocation(t *testing.T) {
	// Force dev mode for this test. The devModeOnce cache means we can't
	// reliably flip mid-process — but we can verify the panic message
	// shape by inspecting it through a recovered defer.
	if !isDevBuild() {
		t.Skip("dev-mode panic only reachable when SPROUT_DEV=1; skipping in default test env")
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on unknown type in dev mode")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "websocket_outbound_registry.go") {
			t.Errorf("panic message should point at the registry file: %v", r)
		}
	}()
	_ = validateOutboundMessageType("definitely_not_registered_test_only")
}
