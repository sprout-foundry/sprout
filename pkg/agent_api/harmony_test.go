package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Constructors
// =====================================================================

func TestNewHarmonyFormatter(t *testing.T) {
	h := NewHarmonyFormatter()
	require.NotNil(t, h)
	assert.Empty(t, h.reasoningLevel)
}

func TestNewHarmonyFormatterWithReasoning(t *testing.T) {
	for _, level := range []string{"low", "medium", "high"} {
		h := NewHarmonyFormatterWithReasoning(level)
		require.NotNil(t, h)
		assert.Equal(t, level, h.reasoningLevel)
	}
}

// =====================================================================
// validateMessages
// =====================================================================

func TestValidateMessages_Empty(t *testing.T) {
	h := NewHarmonyFormatter()
	err := h.validateMessages(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no messages provided")

	err = h.validateMessages([]Message{})
	assert.Error(t, err)
}

func TestValidateMessages_InvalidRole(t *testing.T) {
	h := NewHarmonyFormatter()
	err := h.validateMessages([]Message{{Role: "badrole", Content: "hi"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role")
	assert.Contains(t, err.Error(), "badrole")
}

func TestValidateMessages_EmptyContent(t *testing.T) {
	h := NewHarmonyFormatter()
	err := h.validateMessages([]Message{{Role: "user", Content: "  "}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}

func TestValidateMessages_InvalidRoleAtIndex(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "unknown", Content: "world"},
	}
	err := h.validateMessages(messages)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at message 1")
}

func TestValidateMessages_Valid(t *testing.T) {
	h := NewHarmonyFormatter()
	for _, role := range []string{"system", "user", "assistant", "developer", "tool"} {
		err := h.validateMessages([]Message{{Role: role, Content: "hello"}})
		assert.NoError(t, err, "role %q should be valid", role)
	}
}

func TestValidateMessages_MultipleValid(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	assert.NoError(t, h.validateMessages(messages))
}

// =====================================================================
// formatToolParameters
// =====================================================================

func TestFormatToolParameters_Nil(t *testing.T) {
	h := NewHarmonyFormatter()
	assert.Equal(t, "_: any", h.formatToolParameters(nil))
}

func TestFormatToolParameters_NoProperties(t *testing.T) {
	h := NewHarmonyFormatter()
	params := map[string]interface{}{
		"type": "object",
	}
	assert.Equal(t, "_: any", h.formatToolParameters(params))
}

func TestFormatToolParameters_WithProperties(t *testing.T) {
	h := NewHarmonyFormatter()
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type": "string",
			},
			"count": map[string]interface{}{
				"type": "integer",
			},
		},
	}
	result := h.formatToolParameters(params)
	// Both parameters should appear (order is not guaranteed due to map iteration)
	assert.Contains(t, result, "command?: string")
	assert.Contains(t, result, "count?: integer")
}

func TestFormatToolParameters_RequiredFields(t *testing.T) {
	h := NewHarmonyFormatter()
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type": "string",
			},
			"description": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []interface{}{"command"},
	}
	result := h.formatToolParameters(params)
	assert.Contains(t, result, "command: string")
	assert.Contains(t, result, "description?: string")
}

func TestFormatToolParameters_WrongParamType(t *testing.T) {
	h := NewHarmonyFormatter()
	assert.Equal(t, "_: any", h.formatToolParameters("not a map"))
}

func TestFormatToolParameters_InvalidPropertiesType(t *testing.T) {
	h := NewHarmonyFormatter()
	params := map[string]interface{}{
		"type":       "object",
		"properties": "not a map",
	}
	assert.Equal(t, "_: any", h.formatToolParameters(params))
}

func TestFormatToolParameters_EmptyProperties(t *testing.T) {
	h := NewHarmonyFormatter()
	params := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	assert.Equal(t, "_: any", h.formatToolParameters(params))
}

// =====================================================================
// FormatMessagesForCompletion
// =====================================================================

func TestFormatMessagesForCompletion_NilOpts(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>user<|message|>hello<|end|>")
}

func TestFormatMessagesForCompletion_SystemOnly(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "system", Content: "You are helpful"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>system<|message|>You are helpful<|end|>")
}

func TestFormatMessagesForCompletion_UserOnly(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "What is AI?"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>user<|message|>What is AI?<|end|>")
}

func TestFormatMessagesForCompletion_AssistantOnly(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "assistant", Content: "Hello"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>assistant<|channel|>final<|message|>Hello<|end|>")
}

func TestFormatMessagesForCompletion_DeveloperOnly(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "developer", Content: "Instructions"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>developer<|message|>Instructions<|end|>")
}

func TestFormatMessagesForCompletion_SystemUserConversation(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{
		{Role: "system", Content: "You are a tutor"},
		{Role: "user", Content: "Explain Go"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>system<|message|>You are a tutor<|end|>")
	assert.Contains(t, result, "<|start|>user<|message|>Explain Go<|end|>")
}

func TestFormatMessagesForCompletion_FullConversation(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{
		{Role: "system", Content: "You are a tutor"},
		{Role: "user", Content: "Explain Go"},
		{Role: "assistant", Content: "Go is a programming language"},
		{Role: "user", Content: "What about channels?"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.Contains(t, result, "<|start|>system<|message|>You are a tutor<|end|>")
	assert.Contains(t, result, "<|start|>user<|message|>Explain Go<|end|>")
	assert.Contains(t, result, "<|start|>assistant<|channel|>final<|message|>Go is a programming language<|end|>")
	assert.Contains(t, result, "<|start|>user<|message|>What about channels?<|end|>")
	assert.Contains(t, result, "<|start|>assistant<|channel|>final<|message|>") // trailing prompt
}

func TestFormatMessagesForCompletion_ReasoningLevelLow(t *testing.T) {
	h := NewHarmonyFormatter()
	opts := &HarmonyOptions{ReasoningLevel: "low"}
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	assert.Contains(t, result, "Reasoning: low")
}

func TestFormatMessagesForCompletion_ReasoningLevelMedium(t *testing.T) {
	h := NewHarmonyFormatter()
	opts := &HarmonyOptions{ReasoningLevel: "medium"}
	messages := []Message{{Role: "user", Content: "hello"}}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	// No system message, so reasoning level is not added
	assert.NotContains(t, result, "Reasoning:")
}

func TestFormatMessagesForCompletion_ReasoningLevelHigh(t *testing.T) {
	h := NewHarmonyFormatter()
	opts := &HarmonyOptions{ReasoningLevel: "high"}
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	assert.Contains(t, result, "Reasoning: high")
}

func TestFormatMessagesForCompletion_ReasoningLevelFromFormatter(t *testing.T) {
	h := NewHarmonyFormatterWithReasoning("high")
	opts := &HarmonyOptions{} // empty opts, should use formatter's level
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	assert.Contains(t, result, "Reasoning: high")
}

func TestFormatMessagesForCompletion_InvalidReasoningLevel(t *testing.T) {
	h := NewHarmonyFormatter()
	opts := &HarmonyOptions{ReasoningLevel: "invalid"}
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	// Invalid reasoning level should be ignored
	assert.NotContains(t, result, "Reasoning: invalid")
}

func TestFormatMessagesForCompletion_ReasoningLevelCaseInsensitive(t *testing.T) {
	h := NewHarmonyFormatter()
	opts := &HarmonyOptions{ReasoningLevel: "  HIGH  "}
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	assert.Contains(t, result, "Reasoning: high")
}

func TestFormatMessagesForCompletion_WithOptionsReasoningOverridesFormatter(t *testing.T) {
	h := NewHarmonyFormatterWithReasoning("low")
	opts := &HarmonyOptions{ReasoningLevel: "high"}
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, opts)
	// Opts should override formatter's reasoning level
	assert.Contains(t, result, "Reasoning: high")
	assert.NotContains(t, result, "Reasoning: low")
}

func TestFormatMessagesForCompletion_WithTools(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "shell_command",
				Description: "Run a shell command",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []interface{}{"command"},
				},
			},
		},
	}
	result := h.FormatMessagesForCompletion(messages, tools, nil)
	assert.Contains(t, result, "# Available Tools")
	assert.Contains(t, result, "## functions")
	assert.Contains(t, result, "namespace functions {")
	assert.Contains(t, result, "// Run a shell command")
	assert.Contains(t, result, "type shell_command = (")
	assert.Contains(t, result, "command: string")
	assert.Contains(t, result, "} // namespace functions")
	assert.Contains(t, result, "## Tool Calling Instructions")
	assert.Contains(t, result, "commentary to=functions.")
}

func TestFormatMessagesForCompletion_WithMultipleTools(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "read_file",
				Description: "Read a file",
				Parameters:  nil,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "write_file",
				Description: "Write to a file",
				Parameters:  nil,
			},
		},
	}
	result := h.FormatMessagesForCompletion(messages, tools, nil)
	assert.Contains(t, result, "// Read a file")
	assert.Contains(t, result, "type read_file = ")
	assert.Contains(t, result, "// Write to a file")
	assert.Contains(t, result, "type write_file = ")
}

func TestFormatMessagesForCompletion_ToolNotFunctionType(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	tools := []Tool{
		{
			Type: "not_a_function",
			Function: ToolFunction{
				Name:        "test",
				Description: "Should not appear",
			},
		},
	}
	result := h.FormatMessagesForCompletion(messages, tools, nil)
	// Non-function tool types should be silently skipped
	assert.NotContains(t, result, "type test =")
	assert.Contains(t, result, "# Available Tools")
	assert.Contains(t, result, "namespace functions {")
}

func TestFormatMessagesForCompletion_EmptyTools(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	result := h.FormatMessagesForCompletion(messages, []Tool{}, nil)
	assert.NotContains(t, result, "# Available Tools")
	assert.NotContains(t, result, "## Tool Calling Instructions")
}

func TestFormatMessagesForCompletion_UserMessageNewlineBehavior(t *testing.T) {
	h := NewHarmonyFormatter()
	// User message as the last message should NOT have trailing newlines before the final prompt
	messages := []Message{
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	// The user message should be immediately followed by the final assistant prompt (no extra \n\n)
	// i.e. ...hello<|end|><|start|>assistant...
	assert.Contains(t, result, "<|end|><|start|>assistant")
}

func TestFormatMessagesForCompletion_UserMessageBeforeAssistantNewline(t *testing.T) {
	h := NewHarmonyFormatter()
	// User message followed by assistant should have \n\n between them
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	// The user message should have \n\n after it because it's not the last message
	assert.Contains(t, result, "<|end|>\n\n<|start|>assistant")
}

func TestFormatMessagesForCompletion_SkipsUnknownRoles(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{
		{Role: "tool", Content: "result"}, // tool is valid in validateMessages but not handled in switch
		{Role: "user", Content: "hello"},
	}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	// "tool" role is valid but not rendered in the switch, so only user should appear
	assert.Contains(t, result, "<|start|>user<|message|>hello<|end|>")
}

// =====================================================================
// AddReturnToken
// =====================================================================

func TestAddReturnToken_NoExistingToken(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.AddReturnToken("Hello world")
	assert.Equal(t, "Hello world<|return|>", result)
}

func TestAddReturnToken_ExistingToken(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.AddReturnToken("Hello world<|return|>")
	assert.Equal(t, "Hello world<|return|>", result)
}

func TestAddReturnToken_EmptyString(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.AddReturnToken("")
	assert.Equal(t, "<|return|>", result)
}

// =====================================================================
// ConvertReturnToEnd
// =====================================================================

func TestConvertReturnToEnd_SingleReturn(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.ConvertReturnToEnd("Hello<|return|>World")
	assert.Equal(t, "Hello<|end|>World", result)
}

func TestConvertReturnToEnd_MultipleReturns(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.ConvertReturnToEnd("<|return|>mid<|return|>")
	assert.Equal(t, "<|end|>mid<|end|>", result)
}

func TestConvertReturnToEnd_NoReturns(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.ConvertReturnToEnd("Hello World")
	assert.Equal(t, "Hello World", result)
}

func TestConvertReturnToEnd_EmptyString(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.ConvertReturnToEnd("")
	assert.Equal(t, "", result)
}

// =====================================================================
// StripReturnToken
// =====================================================================

func TestStripReturnToken_RemovesToken(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("Hello world<|return|>")
	assert.Equal(t, "Hello world", result)
}

func TestStripReturnToken_NoToken(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("Hello world")
	assert.Equal(t, "Hello world", result)
}

func TestStripReturnToken_WithWhitespace(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("  Hello <|return|>  ")
	// TrimSpace first: "Hello <|return|>" → TrimSuffix: "Hello " (trailing space from before token)
	assert.Equal(t, "Hello ", result)
}

func TestStripReturnToken_EmptyString(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("")
	assert.Equal(t, "", result)
}

func TestStripReturnToken_OnlyToken(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("<|return|>")
	assert.Equal(t, "", result)
}

func TestStripReturnToken_TokenNotAtEnd(t *testing.T) {
	h := NewHarmonyFormatter()
	result := h.StripReturnToken("<|return|>Hello")
	// TrimSuffix only removes from end, so <|return|> at the start is kept
	assert.Equal(t, "<|return|>Hello", result)
}

// =====================================================================
// Integration: FormatMessagesForCompletion produces valid-looking output
// =====================================================================

func TestFormatMessagesForCompletion_EndsWithAssistantPrompt(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	result := h.FormatMessagesForCompletion(messages, nil, nil)
	assert.True(t, strings.HasSuffix(result, "<|start|>assistant<|channel|>final<|message|>"))
}

func TestFormatMessagesForCompletion_WithToolsEndsWithAssistantPrompt(t *testing.T) {
	h := NewHarmonyFormatter()
	messages := []Message{{Role: "user", Content: "hello"}}
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "test",
				Description: "A test tool",
			},
		},
	}
	result := h.FormatMessagesForCompletion(messages, tools, nil)
	assert.True(t, strings.HasSuffix(result, "<|start|>assistant<|channel|>final<|message|>"))
}
