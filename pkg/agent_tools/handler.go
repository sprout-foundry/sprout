// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// Tools are capabilities the LLM can invoke — reading files, executing shell commands,
// searching code, delegating to subagents, and more. Each tool implements the
// ToolHandler interface and is registered with the ToolRegistry.
//
// # Adding a new tool
//
//  1. Create a new file in this package (e.g., `my_tool_handler.go`).
//  2. Define a struct and implement all ToolHandler methods (Name, Definition,
//     Validate, Execute, plus the 5 optional metadata methods).
//  3. Register it in `AllTools()` in `all.go`.
//
// The subagent tools (run_subagent / run_parallel_subagents) intentionally
// remain in pkg/agent because they need *Agent access for nested runner
// orchestration. See pkg/agent_tools/all.go for the canonical tool list.
package tools

import (
	"context"
	"io"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// BackgroundNotifier is the interface tools use to queue a background
// completion notification. The agent (pkg/agent) implements this so
// tool handlers don't need *Agent access.
type BackgroundNotifier interface {
	NotifyCompletion(sessionID, kind, content string)
}

// ToolHandler defines the interface for a tool that can be invoked by the agent.
type ToolHandler interface {
	// Name returns the unique tool identifier (e.g., "read_file").
	Name() string
	// Definition returns the JSON schema definition for the LLM to understand the tool.
	Definition() ToolDefinition
	// Validate checks arguments before execution. Returns error if invalid.
	Validate(args map[string]any) error
	// Execute runs the tool with the given context, environment, and arguments.
	Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)

	// Metadata — all optional with sensible defaults. When a metadata method
	// returns its zero value, the ToolRegistry falls back to its own
	// registry-wide defaults for timeout and max result size.
	Aliases() []string      // default: nil (no aliases)
	Timeout() time.Duration // default: 0 (use registry default)
	MaxResultSize() int     // default: 0 (use registry default)
	SafeForParallel() bool  // default: false
	Interactive() bool      // default: false
}

// ToolDefinition describes a tool's schema for LLM consumption.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  []ParameterDef `json:"parameters"`
	Required    []string       `json:"required,omitempty"` // Required parameter names

	// Hidden keeps the tool callable but omits it from the roster advertised
	// to the model. Use it for superseded tools: existing callers keep
	// working without the schema costing context on every turn.
	Hidden bool `json:"-"`

	// RequiresEmbeddings marks a tool that has no useful behavior without an
	// embedding index. The registration path filters these out when the
	// agent has no EmbeddingManager, so the model never sees a tool that
	// would fail at execution time.
	RequiresEmbeddings bool `json:"-"`
}

// ParameterDef defines a single tool parameter's schema.
type ParameterDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`

	// Items is the JSON schema for this parameter's elements; only used
	// when Type is "array". It is serialized into the outgoing
	// function-calling schema as the `items` key.
	//
	// Every array parameter MUST declare Items: Gemini 3.x strict
	// function-calling validation rejects the entire request (HTTP 400 via
	// OpenRouter) when any array property lacks `items`. Gemini 2.5 and
	// other providers tolerated the omission. Enforced by
	// TestToolSchemas_ArrayParametersHaveItems.
	Items map[string]any `json:"items,omitempty"`
}

// ToolEnv provides the execution context for a tool without coupling to *Agent.
type ToolEnv struct {
	// EventBus for publishing events (tool_start, tool_end, etc.)
	EventBus *events.EventBus
	// WorkspaceRoot is the working directory root for path resolution
	WorkspaceRoot string
	// OutputWriter for writing tool output (stdout, logs, etc.)
	OutputWriter io.Writer
	// ApprovalManager for security approvals; nil if approvals are not supported
	ApprovalManager ApprovalManager
	// FileAccessClassifier provides Gate 1's path-tier verdict before
	// a file operation runs. Nil means no classifier is available.
	FileAccessClassifier FileAccessClassifier
	// FileAccessPrompter restores the interactive off-workspace approval
	// flow for "prompt" verdicts. When set, handlers consult it before
	// failing with the raw off-workspace error; when nil (no agent
	// context, or a surface that cannot prompt), handlers keep the
	// SP-127 M4 behavior of returning the raw filesystem error.
	FileAccessPrompter FileAccessPrompter // MaxTokensFunc returns the current token budget limit
	MaxTokensFunc      func() int
	// ConfigManager provides configuration access for tools that need it (e.g., API keys for web fetching)
	ConfigManager *configuration.Manager
	// EmbeddingMgr is the agent's long-lived embedding manager. When set,
	// tools must reuse it instead of constructing their own.
	EmbeddingMgr *embedding.EmbeddingManager
	// AskUser routes ask_user prompts through the active interactive channel
	// (WebUI dialog when a browser is connected, terminal stdin otherwise).
	// Nil means the tool must fall back to the CLI prompt directly.
	AskUser AskUserService
	// TodoManager is the conversation-scoped todo list. When nil, tools
	// should fall back to the package-default scope via ManagerForChat("").
	TodoManager *TodoManager
	// IsInteractiveCLI reports whether the agent is running with a controlling
	// TTY (no WebUI client). Tools use this to decide whether to render
	// rich CLI output (boxes, colors) for the user.
	IsInteractiveCLI bool
	// VisionProcessor, when set, lets vision-dependent tools analyze
	// images and UI screenshots without holding an *Agent reference.
	// Nil means the tool must report "vision unavailable".
	VisionProcessor *VisionProcessor
	// WebBrowser runs headless browser navigation (Playwright/rod wrapper).
	// Nil means the tool must report "browser unavailable".
	WebBrowser WebBrowser
	// SkillLoader resolves skill IDs to their on-disk instructions.
	// Nil means skill loading is not available.
	SkillLoader SkillLoader
	// SearchEngine performs Google Custom Search API queries.
	// Nil means web search is not available.
	SearchEngine SearchEngine
	// SubagentDepth is the nesting depth of subagents (0 = primary agent, 1 = first-level
	// subagent, 2 = second-level, etc.). Used by memory gate and other subagent-specific
	// tool behaviors. Default 0 means not in subagent context.
	SubagentDepth int
	// Gate1AutoApproved reports whether Gate 1 already auto-approved this
	// tool call (--unsafe mode or elevated risk profile). When true,
	// handlers skip their interactive approval prompt to avoid double-prompting.
	// Hard blocks are NEVER bypassed regardless of this flag.
	Gate1AutoApproved bool
	// RawArgsJSON is the raw JSON string of the tool arguments as sent by the
	// LLM. When set, handlers can parse this to recover the original key
	// insertion order of nested maps (e.g., the "data" field in
	// write_structured_file) before Go's map iteration randomizes it.
	RawArgsJSON string
	// RepoMapDefaultDepth overrides the repo_map tool's default depth when
	// the caller doesn't specify one. Zero means use the tool's built-in
	// default (3 = full symbols). Low-Context Mode sets this to 1.
	RepoMapDefaultDepth int
	Notifier            BackgroundNotifier
	// LifetimeCtx is a process-scoped context that outlives any single
	// turn. Background goroutines must use this instead of the per-turn
	// ctx so they survive turn boundaries. Cancelled when the agent shuts down.
	LifetimeCtx context.Context
	// ChatID scopes shell sessions (hidden foreground PTY and background
	// PTYs) to the conversation that created them. Empty means the caller
	// has no chat identity (CLI mode, tests) and the shell layer falls
	// back to its legacy "default" bucket.
	ChatID string
	// ToolFuncs carries the agent-dependent tool dispatch closures for the
	// specific agent this env belongs to. When nil, ResolveToolFuncs falls
	// back to the package-level vars (the legacy single-agent path).
	ToolFuncs *ToolFuncSet
	// Agent is the *pkg/agent.Agent instance. Only set for tools that
	// explicitly need agent access (e.g., run_subagent). Nil for all others.
	Agent interface{} `json:"-"`
}

// ToolFuncSet carries the per-agent closures that delegate agent-dependent
// tools (subagent spawn, clarification, change tracking, PR creation,
// automate) back to a specific *Agent instance. The closures are installed
// by pkg/agent's wireAgentToolFuncs at agent construction and travel with
// the agent's ToolEnv, so in a daemon serving multiple agents each tool
// call dispatches to its own agent instead of the most recently constructed
// one (the package-level vars' behavior).
type ToolFuncSet struct {
	RunSubagent          func(ctx context.Context, args map[string]any) (string, error)
	RunParallelSubagents func(ctx context.Context, args map[string]any) (string, error)
	RequestClarification func(ctx context.Context, args map[string]any) (string, error)
	RespondClarification func(ctx context.Context, args map[string]any) (string, error)
	ListChanges          func(ctx context.Context, args map[string]any) (string, error)
	RecoverFile          func(ctx context.Context, args map[string]any) (string, error)
	RevertMyChanges      func(ctx context.Context, args map[string]any) (string, error)
	MCPRefresh           func(ctx context.Context, args map[string]any) (string, error)
	RunAutomate          func(ctx context.Context, args map[string]any) (string, error)
	CreatePullRequest    func(ctx context.Context, args map[string]any) (string, error)
}

// ResolveToolFuncs returns the tool func set to dispatch through. It prefers
// the per-agent set carried in the env; when none is set (callers that build
// ToolEnv directly, e.g. commit_handler's internal ToolEnv{} and existing
// tests), it snapshots the package-level vars under ToolFuncMu so the
// legacy single-agent path keeps working.
func (e ToolEnv) ResolveToolFuncs() *ToolFuncSet {
	if e.ToolFuncs != nil {
		return e.ToolFuncs
	}
	ToolFuncMu.RLock()
	defer ToolFuncMu.RUnlock()
	return &ToolFuncSet{
		RunSubagent:          RunSubagentFunc,
		RunParallelSubagents: RunParallelSubagentsFunc,
		RequestClarification: RequestClarificationFunc,
		RespondClarification: RespondClarificationFunc,
		ListChanges:          ListChangesFunc,
		RecoverFile:          RecoverFileFunc,
		RevertMyChanges:      RevertMyChangesFunc,
		MCPRefresh:           MCPRefreshFunc,
		RunAutomate:          RunAutomateFunc,
		CreatePullRequest:    CreatePullRequestFunc,
	}
}

// AskUserService routes ask_user prompts through the active interactive
// channel (WebUI dialog or CLI stdin). Nil means no input channel is available.
type AskUserService interface {
	// Ask presents req to the user and returns their response.
	Ask(ctx context.Context, req AskUserRequest) (string, error)
}

// ApprovalResult contains the outcome of an approval request.
type ApprovalResult struct {
	Approved    bool   `json:"approved"`
	Reason      string `json:"reason,omitempty"`       // "rejected", "timed_out", "cancelled"
	UserComment string `json:"user_comment,omitempty"` // Optional feedback from user
}

// ApprovalManager handles security approval requests for tool execution.
type ApprovalManager interface {
	// RequestApproval asks the user to approve a tool execution.
	// Returns an ApprovalResult with the outcome and optional context.
	RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) ApprovalResult
}

// FileAccessClassifier provides Gate 1's path-tier verdict before
// running a file operation. It lives in the tool layer so handlers
// can classify a path without importing pkg/agent. Nil means no
// classifier is available (e.g., unit tests).
// FileAccessPrompter surfaces the interactive off-workspace approval
// dialog (WebUI or CLI) for file operations that classified as "prompt".
// pkg/agent implements it on *Agent by delegating to the shared
// handleFileSecurityError flow; pkg/agent_tools consumes it without an
// import cycle.
type FileAccessPrompter interface {
	// PromptFileAccess asks the user to approve out-of-workspace access.
	// toolName is the calling tool; filePath is the user-supplied path;
	// resolvedPath is the canonical target ("" when unresolvable); mode
	// is "read" or "write". Returns a context carrying the security
	// bypass token and true when approved, or the original context and
	// false on deny or when no prompt surface is available.
	PromptFileAccess(ctx context.Context, toolName, filePath, resolvedPath, mode string) (context.Context, bool)
}

type FileAccessClassifier interface {
	// ClassifyFileAccess returns the Gate 1 verdict for a file path:
	// "allow" (proceed), "prompt" (fall through to gate), "deny" (error).
	ClassifyFileAccess(ctx context.Context, filePath, resolvedPath, mode string) string

	// IsFolderSessionAllowed reports whether absPath sits under a folder
	// the user has allowlisted for the rest of the session.
	IsFolderSessionAllowed(absPath string) bool
}

// ---------------------------------------------------------------------------
// Agent subsystem interfaces
// ---------------------------------------------------------------------------

// VisionProcessor is defined in vision_analyze_types.go as a concrete struct.

// WebBrowser provides headless browser navigation for URL/content analysis.
type WebBrowser interface {
	BrowseURL(ctx context.Context, url string, opts map[string]any) (string, error)
}

// SkillInfo describes a skill loaded from disk or embedded.
type SkillInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Source      string `json:"source"` // "builtin", "user", or "project"
}

// SkillLoader resolves skill IDs to their on-disk instructions.
type SkillLoader interface {
	// LoadSkill resolves a skill ID and returns its metadata and content.
	LoadSkill(skillID string) (*SkillInfo, error)
}

// SearchEngine performs web search queries via Google Custom Search API.
type SearchEngine interface {
	// Search runs a web search query and returns formatted results.
	Search(ctx context.Context, query string) (string, error)
}

// ImageData represents an image returned by a vision-capable tool.
type ImageData struct {
	// URI is the path or data URI of the image
	URI string `json:"uri"`
	// Base64 is the base64-encoded image data (for inline multimodal attachment)
	Base64 string `json:"base64,omitempty"`
	// MIMEType is the image MIME type (e.g., "image/png")
	MIMEType string `json:"mime_type"`
}

// ToolResult is the return value from a tool's Execute method.
type ToolResult struct {
	// Output is the primary text result of the tool execution.
	Output string `json:"output"`
	// StructuredOut holds optional structured data (maps, slices, etc.)
	StructuredOut any `json:"structured_out,omitempty"`
	// Images contains optional image data for vision-capable tools.
	Images []ImageData `json:"images,omitempty"`
	// TokenUsage tracks tokens consumed during execution.
	TokenUsage int64 `json:"token_usage"`
	// IsError indicates whether this result represents an error state.
	IsError bool `json:"is_error"`
}
