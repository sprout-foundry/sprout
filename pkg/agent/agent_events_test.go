package agent

import (
	"testing"
)

func TestDecorateEventPayload_NilData(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	result := a.decorateEventPayload(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestDecorateEventPayload_NoEventMetadata(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	input := map[string]interface{}{"key": "value"}
	result := a.decorateEventPayload(input)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	outMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if outMap["key"] != "value" {
		t.Errorf("expected key=value, got %v", outMap["key"])
	}
}

func TestDecorateEventPayload_MergesEventMetadata(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.SetEventMetadata(map[string]interface{}{
		"client_id": "client-123",
		"chat_id":   "chat-456",
		"source":    "webui",
	})

	input := map[string]interface{}{
		"message":  "hello",
		"category": "test",
	}
	result := a.decorateEventPayload(input)
	outMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if outMap["message"] != "hello" {
		t.Errorf("expected message=hello, got %v", outMap["message"])
	}
	if outMap["client_id"] != "client-123" {
		t.Errorf("expected client_id=client-123, got %v", outMap["client_id"])
	}
	if outMap["chat_id"] != "chat-456" {
		t.Errorf("expected chat_id=chat-456, got %v", outMap["chat_id"])
	}
	if outMap["source"] != "webui" {
		t.Errorf("expected source=webui, got %v", outMap["source"])
	}
}

func TestDecorateEventPayload_PayloadKeyNotOverwritten(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.SetEventMetadata(map[string]interface{}{
		"message": "overwritten",
	})

	input := map[string]interface{}{
		"message": "original",
	}
	result := a.decorateEventPayload(input)
	outMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if outMap["message"] != "original" {
		t.Errorf("expected payload key preserved as 'original', got %v", outMap["message"])
	}
}

func TestDecorateEventPayload_FunctionValuesResolved(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.SetEventMetadata(map[string]interface{}{
		"dynamic_field": func() string { return "resolved_value" },
		"static_field":  "static_value",
	})

	input := map[string]interface{}{
		"key": "value",
	}
	result := a.decorateEventPayload(input)
	outMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if outMap["dynamic_field"] != "resolved_value" {
		t.Errorf("expected function resolved to 'resolved_value', got %v", outMap["dynamic_field"])
	}
	if outMap["static_field"] != "static_value" {
		t.Errorf("expected 'static_value', got %v", outMap["static_field"])
	}
}

func TestDecorateEventPayload_NonMapData(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.SetEventMetadata(map[string]interface{}{
		"client_id": "client-123",
	})

	// String data should pass through unchanged
	strResult := a.decorateEventPayload("hello")
	if strResult != "hello" {
		t.Errorf("expected 'hello', got %v", strResult)
	}

	// Int data should pass through unchanged
	intResult := a.decorateEventPayload(42)
	if intResult != 42 {
		t.Errorf("expected 42, got %v", intResult)
	}

	// Slice data should pass through unchanged
	sliceResult := a.decorateEventPayload([]string{"a", "b"})
	if len(sliceResult.([]string)) != 2 {
		t.Errorf("expected slice of length 2")
	}
}

func TestDecorateEventPayload_CloneIsolation(t *testing.T) {
	t.Parallel()
	a := &Agent{
		output: NewAgentOutputManager(),
	}
	a.SetEventMetadata(map[string]interface{}{
		"meta_key": "meta_value",
	})

	input := map[string]interface{}{
		"payload_key": "payload_value",
	}
	result1 := a.decorateEventPayload(input)
	outMap1 := result1.(map[string]interface{})
	outMap1["tampered"] = true

	// Second call should not see the tamper
	result2 := a.decorateEventPayload(input)
	outMap2 := result2.(map[string]interface{})
	if _, exists := outMap2["tampered"]; exists {
		t.Errorf("expected cloned result, not shared reference")
	}
}

func TestGetEventClientID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		meta     map[string]interface{}
		expected string
	}{
		{name: "with client_id", meta: map[string]interface{}{"client_id": "abc-123"}, expected: "abc-123"},
		{name: "empty metadata", meta: map[string]interface{}{}, expected: ""},
		{name: "nil metadata", meta: nil, expected: ""},
		{name: "client_id with spaces", meta: map[string]interface{}{"client_id": "  spaces "}, expected: "spaces"},
		{name: "client_id not string type", meta: map[string]interface{}{"client_id": 123}, expected: ""},
		{name: "client_id absent", meta: map[string]interface{}{"other": "value"}, expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{
				output: NewAgentOutputManager(),
			}
			if tc.meta != nil {
				a.SetEventMetadata(tc.meta)
			}
			if got := a.GetEventClientID(); got != tc.expected {
				t.Errorf("GetEventClientID() = %q, expected %q", got, tc.expected)
			}
		})
	}
}

func TestGetEventChatID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		meta     map[string]interface{}
		expected string
	}{
		{name: "with chat_id", meta: map[string]interface{}{"chat_id": "chat-456"}, expected: "chat-456"},
		{name: "empty metadata", meta: map[string]interface{}{}, expected: ""},
		{name: "nil metadata", meta: nil, expected: ""},
		{name: "chat_id with spaces", meta: map[string]interface{}{"chat_id": "  trimmed "}, expected: "trimmed"},
		{name: "chat_id not string type", meta: map[string]interface{}{"chat_id": 999}, expected: ""},
		{name: "chat_id absent", meta: map[string]interface{}{"other": "value"}, expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{
				output: NewAgentOutputManager(),
			}
			if tc.meta != nil {
				a.SetEventMetadata(tc.meta)
			}
			if got := a.GetEventChatID(); got != tc.expected {
				t.Errorf("GetEventChatID() = %q, expected %q", got, tc.expected)
			}
		})
	}
}

func TestGetEventUserID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		meta     map[string]interface{}
		expected string
	}{
		{name: "with user_id", meta: map[string]interface{}{"user_id": "user-789"}, expected: "user-789"},
		{name: "empty metadata", meta: map[string]interface{}{}, expected: ""},
		{name: "nil metadata", meta: nil, expected: ""},
		{name: "user_id with spaces", meta: map[string]interface{}{"user_id": "  trimmed "}, expected: "trimmed"},
		{name: "user_id not string type", meta: map[string]interface{}{"user_id": 123}, expected: ""},
		{name: "user_id absent", meta: map[string]interface{}{"other": "value"}, expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{
				output: NewAgentOutputManager(),
			}
			if tc.meta != nil {
				a.SetEventMetadata(tc.meta)
			}
			if got := a.GetEventUserID(); got != tc.expected {
				t.Errorf("GetEventUserID() = %q, expected %q", got, tc.expected)
			}
		})
	}
}
