package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// newCacheTestProvider builds a minimal GenericProvider suitable for testing
// cache_control injection in convertMessages and buildChatRequest. Only the
// fields exercised by those code paths are populated.
func newCacheTestProvider(cacheControl bool) *GenericProvider {
	return &GenericProvider{
		config: &ProviderConfig{
			Name: "test",
			Conversion: MessageConversion{
				CacheControl: cacheControl,
			},
		},
		model: "test-model",
	}
}

func cacheTestMessages() []api.Message {
	return []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello!"},
	}
}

func cacheTestTools() []api.Tool {
	return []api.Tool{
		{
			Type: "function",
			Function: api.ToolFunction{
				Name:        "tool_one",
				Description: "First tool",
				Parameters: map[string]interface{}{
					"type": "object",
				},
			},
		},
		{
			Type: "function",
			Function: api.ToolFunction{
				Name:        "tool_two",
				Description: "Second tool",
				Parameters: map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
}

// TestConvertMessages_NoCacheControlByDefault verifies that when CacheControl
// is false (the default), no cache_control key is injected into any message.
func TestConvertMessages_NoCacheControlByDefault(t *testing.T) {
	provider := newCacheTestProvider(false)

	converted := provider.convertMessages(cacheTestMessages(), "")

	if len(converted) != 2 {
		t.Fatalf("expected 2 converted messages, got %d", len(converted))
	}

	for i, msg := range converted {
		if _, ok := msg["cache_control"]; ok {
			t.Errorf("message %d (role=%v) should NOT have cache_control when CacheControl=false", i, msg["role"])
		}
	}
}

// TestConvertMessages_CacheControlOnSystem verifies that when CacheControl is
// true, the system message gets a cache_control: {type: "ephemeral"} marker
// and non-system messages do not.
func TestConvertMessages_CacheControlOnSystem(t *testing.T) {
	provider := newCacheTestProvider(true)

	converted := provider.convertMessages(cacheTestMessages(), "")

	if len(converted) != 2 {
		t.Fatalf("expected 2 converted messages, got %d", len(converted))
	}

	// First message (system) must carry cache_control.
	sysMsg := converted[0]
	if role, _ := sysMsg["role"].(string); role != "system" {
		t.Fatalf("expected first message to be system role, got %q", role)
	}
	cc, ok := sysMsg["cache_control"]
	if !ok {
		t.Fatal("system message should have cache_control when CacheControl=true")
	}
	ccMap, ok := cc.(map[string]interface{})
	if !ok {
		t.Fatalf("cache_control should be a map, got %T", cc)
	}
	if ccMap["type"] != "ephemeral" {
		t.Errorf("expected cache_control type 'ephemeral', got %v", ccMap["type"])
	}

	// User message must NOT carry cache_control.
	userMsg := converted[1]
	if _, ok := userMsg["cache_control"]; ok {
		t.Error("non-system message should NOT have cache_control")
	}
}

// TestConvertMessages_CacheControlNoSystemMessage verifies graceful no-op when
// CacheControl is enabled but there is no system message in the conversation.
func TestConvertMessages_CacheControlNoSystemMessage(t *testing.T) {
	provider := newCacheTestProvider(true)

	messages := []api.Message{
		{Role: "user", Content: "No system message here."},
	}

	// Must not panic and must not inject cache_control.
	converted := provider.convertMessages(messages, "")

	if len(converted) != 1 {
		t.Fatalf("expected 1 converted message, got %d", len(converted))
	}
	if _, ok := converted[0]["cache_control"]; ok {
		t.Error("non-system message should not get cache_control")
	}
}

// TestBuildChatRequest_CacheControlTools verifies that when CacheControl is
// true and tools are present, the LAST tool in the request gets a
// cache_control marker while the others do not.
func TestBuildChatRequest_CacheControlTools(t *testing.T) {
	provider := newCacheTestProvider(true)
	tools := cacheTestTools()

	body, err := provider.buildChatRequest(cacheTestMessages(), tools, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest failed: %v", err)
	}

	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	toolsRaw, ok := request["tools"]
	if !ok {
		t.Fatal("expected tools in request body")
	}
	toolList, ok := toolsRaw.([]interface{})
	if !ok {
		t.Fatalf("expected tools to be a slice, got %T", toolsRaw)
	}
	if len(toolList) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(toolList))
	}

	// First tool must NOT have cache_control.
	firstTool := toolList[0].(map[string]interface{})
	if _, ok := firstTool["cache_control"]; ok {
		t.Error("first tool should NOT have cache_control")
	}

	// Last tool MUST have cache_control.
	lastTool := toolList[1].(map[string]interface{})
	cc, ok := lastTool["cache_control"]
	if !ok {
		t.Fatal("last tool should have cache_control when CacheControl=true")
	}
	ccMap, ok := cc.(map[string]interface{})
	if !ok {
		t.Fatalf("cache_control should be a map, got %T", cc)
	}
	if ccMap["type"] != "ephemeral" {
		t.Errorf("expected cache_control type 'ephemeral', got %v", ccMap["type"])
	}
}

// TestBuildChatRequest_NoCacheControlTools verifies that when CacheControl is
// false, tools are serialized normally without any cache_control markers.
func TestBuildChatRequest_NoCacheControlTools(t *testing.T) {
	provider := newCacheTestProvider(false)
	tools := cacheTestTools()

	body, err := provider.buildChatRequest(cacheTestMessages(), tools, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest failed: %v", err)
	}

	bodyStr := string(body)
	if strings.Contains(bodyStr, "cache_control") {
		t.Errorf("request body should NOT contain cache_control when CacheControl=false, got: %s", bodyStr)
	}
}

// TestBuildChatRequest_CacheControlSystemMessage verifies the full request
// body contains cache_control on the system message when CacheControl is true.
func TestBuildChatRequest_CacheControlSystemMessage(t *testing.T) {
	provider := newCacheTestProvider(true)

	body, err := provider.buildChatRequest(cacheTestMessages(), nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest failed: %v", err)
	}

	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	messagesRaw, ok := request["messages"]
	if !ok {
		t.Fatal("expected messages in request body")
	}
	msgList, ok := messagesRaw.([]interface{})
	if !ok {
		t.Fatalf("expected messages to be a slice, got %T", messagesRaw)
	}
	if len(msgList) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgList))
	}

	sysMsg := msgList[0].(map[string]interface{})
	cc, ok := sysMsg["cache_control"]
	if !ok {
		t.Fatal("system message should have cache_control in request body")
	}
	ccMap, ok := cc.(map[string]interface{})
	if !ok {
		t.Fatalf("cache_control should be a map, got %T", cc)
	}
	if ccMap["type"] != "ephemeral" {
		t.Errorf("expected cache_control type 'ephemeral', got %v", ccMap["type"])
	}
}
