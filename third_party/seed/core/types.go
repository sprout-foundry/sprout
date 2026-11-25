package core

// MetaKeyCheckpoint is the Meta key set on messages inserted by checkpoint
// compaction so they can be identified without string matching.
const MetaKeyCheckpoint = "checkpoint"

// MetaKeyMsgID is the Meta key carrying a stable per-state identity for a
// message. AddMessage stamps every appended message with a monotonically
// increasing ID so consumers can match messages across list rebuilds
// (compaction persists, index rebases) without relying on position or
// content equality — duplicate contents are common in long agent sessions
// (identical nudge texts, repeated file reads) and positional indices
// shift under compaction. Never serialized (Meta is json:"-").
const MetaKeyMsgID = "msg.id"

// Tool status values for the Message.Status field. Set by an Executor on
// the Message returned for a tool call; read by the chat loop when it
// publishes the EventTypeToolEnd event so consumers (CLI tool log, WebUI
// status badges) can distinguish successful executions from failures.
//
// An empty Status (zero value) is treated as ToolStatusCompleted by the
// chat loop — Executors that don't tag results keep the historical
// "always completed" behavior.
const (
	// ToolStatusCompleted indicates the tool ran and returned a normal
	// result. This is the default published when Status is unset.
	ToolStatusCompleted = "completed"

	// ToolStatusError indicates the tool failed before or during execution.
	// Reasons include: argument parse errors, circuit-breaker rejections,
	// pre-execute hook rejections, handler errors, and execution timeouts.
	// The error detail is in Message.Content.
	ToolStatusError = "error"
)

// Message represents a single message in a conversation.
type Message struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	Images           []ImageData       `json:"images,omitempty"`
	Meta             map[string]string `json:"-"`

	// Status, when set on a Message returned from a ToolExecutor, indicates
	// how a tool call resolved. Use the ToolStatus* constants. The chat loop
	// reads this when publishing EventTypeToolEnd so consumers can render
	// success vs error indicators. Empty defaults to ToolStatusCompleted —
	// keeps the field opt-in for Executors that don't tag results.
	Status string `json:"status,omitempty"`
}

// SetMeta sets a key-value pair in the Meta map, initializing it if nil.
func (m *Message) SetMeta(key, value string) {
	if m.Meta == nil {
		m.Meta = make(map[string]string)
	}
	m.Meta[key] = value
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction represents the function details of a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool represents a tool definition that can be provided to the model.
// The structure matches the OpenAI function-calling wire format where
// tool definitions are nested under a "function" key.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a tool's identity and parameter schema.
// Parameters is an interface{} to accept any JSON schema structure
// (seed's ToolParameters for native tools, or arbitrary schemas from providers).
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolParameters defines the schema for a tool's arguments.
type ToolParameters struct {
	Type       string                   `json:"type"`
	Properties map[string]ToolParameter `json:"properties"`
	Required   []string                 `json:"required,omitempty"`
}

// ToolParameter defines a single parameter within a tool's schema.
type ToolParameter struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ImageData represents an image to include in a message.
// Images are typically provided as base64-encoded strings.
type ImageData struct {
	URL    string `json:"url,omitempty"`
	Base64 string `json:"base64,omitempty"`
	Type   string `json:"type,omitempty"` // MIME type (image/jpeg, image/png, etc.)
}

// ChatRequest is a request to the chat completion endpoint.
type ChatRequest struct {
	Model      string    `json:"model,omitempty"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
	Stream     bool      `json:"stream,omitempty"`
}

// ChatResponse is a response from the chat completion endpoint.
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object,omitempty"`  // e.g. "chat.completion"
	Created int64        `json:"created,omitempty"` // Unix timestamp
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

// ChatChoice represents one possible completion from the model.
type ChatChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ChatUsage tracks token usage and cost for a request/response.
type ChatUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	// CachedTokens is the number of prompt tokens served from cache (OpenRouter).
	CachedTokens int `json:"cached_tokens,omitempty"`
	// CacheWriteTokens is the number of prompt tokens written to cache (OpenRouter).
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// ImageTokens is the number of tokens consumed by image inputs (vision models).
	ImageTokens int `json:"image_tokens,omitempty"`
}

// AgentState tracks the state of an agent's conversation.
type AgentState struct {
	Messages         []Message        `json:"messages"`
	SessionID        string           `json:"session_id"`
	TotalTokens      int              `json:"total_tokens"`
	TotalCost        float64          `json:"total_cost"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	Checkpoints      []TurnCheckpoint `json:"checkpoints,omitempty"`
}

// ToMessage returns the first choice's message from the response, or an empty message.
func (r ChatResponse) ToMessage() Message {
	if len(r.Choices) == 0 {
		return Message{}
	}
	return r.Choices[0].Message
}
