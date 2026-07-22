// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// Tools are capabilities the LLM can invoke — reading files, executing shell commands,
// searching code, delegating to subagents, automating browsers, and more. Each tool
// implements the ToolHandler interface and is registered with the ToolRegistry.
//
// # ToolHandler interface
//
// The ToolHandler interface replaced the legacy `type ToolHandler func(ctx, args,
// agent) (images, output, error)` func type. The old func-based system tightly coupled
// every tool to the *Agent type, making tools hard to test in isolation and difficult
// to share across different execution contexts. The new interface-based system provides
// explicit dependencies through ToolEnv, enabling clean separation of concerns:
//
//	type ToolHandler interface {
//	    Name() string
//	    Definition() ToolDefinition
//	    Validate(args map[string]any) error
//	    Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)
//	}
//
// # Adding a new tool
//
//  1. Create a new file in this package (e.g., `my_tool_handler.go`).
//  2. Define a struct and implement all ToolHandler methods (Name, Definition,
//     Validate, Execute, plus the 5 optional metadata methods).
//  3. Register it in `AllTools()` in `all.go`.
//
// See AGENTS.md for tool documentation and conventions.
//
// # Migration from legacy func-style handlers
//
// The legacy tool system used function types directly coupled to *Agent. The new
// interface-based system decouples tools from the agent via ToolEnv, which provides
// explicit dependencies (EventBus, WorkspaceRoot, OutputWriter, etc.).
//
// During the migration period, a dual-dispatch shim in pkg/agent/tool_definitions.go
// bridges both systems: when ExecuteTool() is called, it first checks the new registry
// via tools.GetNewToolRegistry().Lookup(name). If a handler is found there, it builds
// a ToolEnv from the agent context and dispatches through the new interface. If no
// handler exists in the new registry, it falls back to the legacy func-style handlers.
// This allows incremental migration without breaking existing functionality.
//
// The subagent tools (run_subagent / run_parallel_subagents) intentionally
// remain in the seed registry under pkg/agent because they need *Agent
// access for nested runner orchestration. See pkg/agent_tools/all.go for
// the canonical tool list.
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
}

// ParameterDef defines a single tool parameter's schema.
type ParameterDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
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
	// FilesystemGate bridges file-touching handlers to the agent's
	// off-workspace approval flow. When non-nil, handlers call it on
	// SafeResolve* errors so the user can approve, session-allowlist,
	// or elevate instead of receiving a hard error. Nil means the
	// handler runs in a context without filesystem gate support
	// (unit tests, agents constructed without the seed dispatch path).
	FilesystemGate FilesystemGate
	// FileAccessClassifier provides Gate 1's path-tier verdict before
	// a file operation runs. When non-nil, handlers call it up-front
	// (SP-127 M2) so Deny is caught immediately with a typed error,
	// Allow skips the gate prompt entirely, and Prompt falls through
	// to withFilesystemApproval for the interactive approval dialog.
	// Nil means no classifier is available; handlers fall through
	// to withFilesystemApproval which also delegates to the classifier.
	FileAccessClassifier FileAccessClassifier
	// MaxTokensFunc returns the current token budget limit
	MaxTokensFunc func() int
	// ConfigManager provides configuration access for tools that need it (e.g., API keys for web fetching)
	ConfigManager *configuration.Manager
	// EmbeddingMgr is the agent's long-lived embedding manager. When set, tools
	// must reuse it instead of constructing their own — the manager holds the
	// loaded ONNX model and an open HNSW handle, so per-call construction is
	// both slow and unsafe under concurrent writes.
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
	// Gate1AutoApproved reports whether Gate 1 (ExecuteTool via
	// staticGateAutoApprove) already auto-approved this tool call. This
	// happens when the agent is in --unsafe mode OR the session risk
	// profile is elevated (permissive/unrestricted) for a non-hard-block
	// operation. When true, handlers that run their own security
	// classification (Gate 2) skip their interactive approval prompt to
	// match Gate 1's decision and avoid double-prompting.
	//
	// Hard blocks (IsHardBlock) are NEVER bypassed regardless of this
	// flag — staticGateAutoApprove returns true for hard blocks only under
	// full --unsafe mode, and handlers must still honor their own
	// IsHardBlock early-returns.
	Gate1AutoApproved bool
	// RawArgsJSON is the raw JSON string of the tool arguments as sent by the
	// LLM. When set, handlers can parse this to recover the original key
	// insertion order of nested maps (e.g., the "data" field in
	// write_structured_file) before Go's map iteration randomizes it.
	RawArgsJSON string
	// RepoMapDefaultDepth (SP-125) overrides the repo_map tool's default
	// depth when the caller doesn't specify one. Zero means use the tool's
	// built-in default (3 = full symbols). Low-Context Mode sets this to 1
	// (directory tree only) to keep repo_map output under ~800 tokens.
	RepoMapDefaultDepth int
	Notifier            BackgroundNotifier
	// Agent is the *pkg/agent.Agent instance. Only set for tools that explicitly
	// need agent access (e.g., run_subagent, run_parallel_subagents).
	// For all other tools this is nil. Use with care — it creates a tight
	// coupling that should be avoided for new tools.
	Agent interface{} `json:"-"`
}

// AskUserService is the interface ask_user-style tools use to drive an
// interactive prompt. Implementations decide between WebUI routing
// (event bus + AskUserManager) and CLI stdin fallback based on whether
// a browser client is connected. ToolEnv.AskUser is populated by the
// agent at dispatch time so the tool handler doesn't need *Agent.
type AskUserService interface {
	// Ask presents req to the user and returns their response. Returns
	// ErrAskUserNoChannel when no input channel is available so callers
	// can surface a structured error to the LLM.
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

// FilesystemGate is the bridge between file-touching tool handlers and
// the agent's filesystem security policy. The tool layer (this package)
// must not import pkg/agent directly — that would create a cycle — so
// handlers receive the gate through ToolEnv and call it when
// SafeResolvePath[ForWrite]WithBypass returns ErrOutsideWorkingDirectory
// or ErrWriteOutsideWorkingDirectory.
//
// A nil FilesystemGate is the test/non-agent default: handlers should
// fall through and return the error to the caller (preserving the
// historical behavior for tests that bypass the agent context).
//
// When non-nil, RequestPathApproval classifies the path (Sensitive vs
// External), presents the appropriate dialog through the active
// surface (CLI picker or WebUI event bus), and returns a context with
// the security bypass token set if the user approved. The caller
// retries the original operation with the returned context; if the
// retry still fails, the operation aborts.
//
// Approval choices:
//
//   - Deny               → returns the original ctx, approved=false.
//   - Approve (once)     → returns wrapped ctx, approved=true. No
//                          persistence; the next access re-prompts.
//   - Allow folder
//     (this session)     → External tier only: persists the folder to
//                          the agent's session allowlist so subsequent
//                          writes under the same folder skip the
//                          prompt for the rest of the session.
//   - Elevate (session)  → marks the agent's risk profile permissive
//                          so the static classifier + this gate + the
//                          shell cascade all skip prompts until the
//                          session ends.
//
// Sensitive-tier paths never enter the allowlist — they're always
// one-shot, even when the client requests a session allow. The
// implementation is responsible for that invariant (the existing
// applyFilesystemDecision does this).
//
// `filePath` is the user-supplied path the LLM passed to the tool.
// `resolvedPath` is the canonical target after symlink resolution
// (i.e. the actual filesystem object that would be touched). The
// adapter displays both so the user can verify the destination is
// what they expect — a symlink `workspace/link` pointing to
// `/etc/passwd` would otherwise be approved without the user
// noticing the resolved target. Pass "" for `resolvedPath` when
// the caller cannot compute it (it will be omitted from display).
type FilesystemGate interface {
	// RequestPathApproval asks the user to approve an off-workspace
	// file access. Returns a possibly-wrapped context and a bool —
	// true means the caller may retry the operation with the returned
	// context. False means deny; surface the original error to the
	// caller.
	RequestPathApproval(ctx context.Context, toolName, filePath, resolvedPath string, err error) (context.Context, bool)
}

// FileAccessClassifier is the interface through which tool handlers
// consult Gate 1's path-tier decision before running a file operation.
// It lives in the tool layer (this package) so handlers in
// pkg/agent_tools can classify a path without importing pkg/agent.
//
// Implementations are responsible for the full allow/prompt/deny
// decision, including workspace containment, /tmp short-circuit,
// session allowlist checks, sensitive-path checks, and read_only
// enforcement. A nil classifier means no Gate 1 context is available
// (e.g., unit tests); handlers must fall through to withFilesystemApproval
// when nil, preserving the historical behavior.
//
// SP-127 M2: handlers call ClassifyFileAccess at the top of Execute
// so Gate 1 sees the path on the FIRST call, not after
// SafeResolvePath fails. The result controls whether to proceed
// directly (Allow), fall through to withFilesystemApproval (Prompt),
// or return a typed denial (Deny).
type FileAccessClassifier interface {
	// ClassifyFileAccess returns the Gate 1 verdict for a file path.
	// Inputs match classifyFileAccess: filePath (user-supplied),
	// resolvedPath (symlink-evaluated canonical form, may equal filePath),
	// mode ("read" or "write").
	// Returns: "allow" (proceed), "prompt" (fall through to gate), "deny" (return error).
	// ctx carries the audit logger; implementations log the verdict via LogJSON
	// so every decision (allow/prompt/deny) appears in the audit trail.
	ClassifyFileAccess(ctx context.Context, filePath, resolvedPath, mode string) string
}

// ---------------------------------------------------------------------------
// SP-079-1: Agent subsystem interfaces
// ---------------------------------------------------------------------------

// VisionProcessor is defined in vision_analyze_types.go as a concrete struct.
// We use the pointer directly here — no separate interface needed since
// the type already lives in this package.

// WebBrowser provides headless browser navigation for URL/content analysis.
type WebBrowser interface {
	// BrowseURL navigates to a URL and returns rendered content.
	// The opts parameter carries tool arguments (action, viewport dimensions,
	// selectors, steps, etc.) as a flexible map. Implementations are expected
	// to convert this map into their internal option struct (e.g.
	// webcontent.BrowseOptions) and perform any action-specific validation.
	BrowseURL(ctx context.Context, url string, opts map[string]any) (string, error)
}

// SkillInfo is the canonical description of a skill loaded from disk or
// embedded. It lives here (rather than in pkg/agent) so that pkg/agent_tools
// can reference it without creating an import cycle.
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
