// Package api provides API types used across all providers.
//
// Seed canonical types (github.com/sprout-foundry/seed/core) are imported
// via type aliases. Sprout consumes these types — it does not define them.
package api

import (
	"context"

	core "github.com/sprout-foundry/seed/core"
)

// ---------------------------------------------------------------------------
// Canonical types — type aliases to seed/core.
// Seed owns the type definitions; sprout consumes them.
// ---------------------------------------------------------------------------

type ImageData = core.ImageData
type Message = core.Message
type ToolCall = core.ToolCall
type ToolCallFunction = core.ToolCallFunction
type Tool = core.Tool
type ToolFunction = core.ToolFunction
type ToolParameters = core.ToolParameters
type ToolParameter = core.ToolParameter
type ChatRequest = core.ChatRequest
type ChatResponse = core.ChatResponse
type ChatChoice = core.ChatChoice
type ChatUsage = core.ChatUsage
type Choice = ChatChoice

// ---------------------------------------------------------------------------
// Provider and client interfaces — sprout-specific, not part of seed.
// ---------------------------------------------------------------------------

// ProviderInterface defines the interface that all providers must implement.
// Mirrors ClientInterface's ctx-bearing send methods — see SP-034.
type ProviderInterface interface {
	SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
	SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error)
	CheckConnection() error
	SetDebug(debug bool)
	SetModel(model string) error
	GetModel() string
	GetProvider() string
	GetModelContextLimit() (int, error)
	ListModels(ctx context.Context) ([]ModelInfo, error)
	SupportsVision() bool
	SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
}

// Note: StreamCallback and ModelInfo are defined in streaming.go and models.go
// respectively to avoid circular import issues.
