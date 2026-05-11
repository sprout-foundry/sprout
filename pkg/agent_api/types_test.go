package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTPSBase_New(t *testing.T) {
	base := NewTPSBase()
	assert.NotNil(t, base)
	assert.NotNil(t, base.tpsTracker)
}

func TestTPSBase_GetLastTPS(t *testing.T) {
	base := NewTPSBase()
	// Initially should be 0
	tps := base.GetLastTPS()
	assert.Equal(t, 0.0, tps)
}

func TestTPSBase_GetAverageTPS(t *testing.T) {
	base := NewTPSBase()
	tps := base.GetAverageTPS()
	assert.Equal(t, 0.0, tps)
}

func TestTPSBase_GetTPSStats(t *testing.T) {
	base := NewTPSBase()
	stats := base.GetTPSStats()
	assert.NotNil(t, stats)
}

func TestTPSBase_ResetTPSStats(t *testing.T) {
	base := NewTPSBase()
	assert.NotPanics(t, func() {
		base.ResetTPSStats()
	})
}

func TestTPSBase_GetTracker_NilSafe(t *testing.T) {
	base := &TPSBase{}
	tracker := base.GetTracker()
	assert.NotNil(t, tracker, "GetTracker should initialize tracker if nil")
}

func TestMessage_JSONFields(t *testing.T) {
	m := Message{
		Role:             "user",
		Content:          "hello",
		ReasoningContent: "thinking...",
		Images: []ImageData{
			{URL: "http://example.com/img.png", Type: "image/png"},
		},
		ToolCallId: "tc-123",
		ToolCalls: []ToolCall{
			{ID: "call-1", Type: "function"},
		},
	}
	assert.Equal(t, "user", m.Role)
	assert.Equal(t, "hello", m.Content)
	assert.Len(t, m.Images, 1)
	assert.Len(t, m.ToolCalls, 1)
}

func TestToolCall_Struct(t *testing.T) {
	tc := ToolCall{
		ID:   "call-1",
		Type: "function",
	}
	tc.Function.Name = "shell_command"
	tc.Function.Arguments = `{"command":"ls"}`
	assert.Equal(t, "shell_command", tc.Function.Name)
	assert.Contains(t, tc.Function.Arguments, "ls")
}

func TestChoice_Struct(t *testing.T) {
	c := Choice{
		Index:        0,
		FinishReason: "stop",
	}
	c.Message.Role = "assistant"
	c.Message.Content = "response text"
	assert.Equal(t, 0, c.Index)
	assert.Equal(t, "stop", c.FinishReason)
	assert.Equal(t, "assistant", c.Message.Role)
}

func TestChatResponse_Struct(t *testing.T) {
	r := ChatResponse{
		ID:      "resp-1",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
	}
	r.Usage.PromptTokens = 100
	r.Usage.CompletionTokens = 50
	r.Usage.TotalTokens = 150
	assert.Equal(t, "resp-1", r.ID)
	assert.Equal(t, 150, r.Usage.TotalTokens)
}

func TestImageData_Struct(t *testing.T) {
	img := ImageData{
		URL:    "http://example.com/img.png",
		Base64: "iVBORw0KGgo=",
		Type:   "image/png",
	}
	assert.Equal(t, "image/png", img.Type)
	assert.NotEmpty(t, img.Base64)
}
