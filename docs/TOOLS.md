# Tool System

This document explains the interface-based tool system used by the Sprout AI agent.
Tools are capabilities the LLM can invoke — file operations, shell commands, subagent
delegation, browser automation, and more.

---

## Overview

The tool system is built around four core concepts:

| Concept | Location | Purpose |
|---------|----------|---------|
| **ToolHandler** interface | `pkg/agent_tools/handler.go` | Contract every tool must implement |
| **ToolRegistry** | `pkg/agent_tools/registry.go` | Thread-safe registration and lookup |
| **ToolEnv** | `pkg/agent_tools/handler.go` | Execution context passed to tools |
| **AllTools()** | `pkg/agent_tools/all.go` | Central registration list |

Tools are defined in the `pkg/agent_tools/` package, one struct per file.

---

## How to Add a Tool

### Step 1: Create a new file in `pkg/agent_tools/`

Each tool lives in its own file named `<tool_name>_handler.go`, for example
`pkg/agent_tools/greet_handler.go`.

### Step 2: Define a struct that implements the ToolHandler interface

```go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type greetHandler struct{}

func (h *greetHandler) Name() string {
	return "greet"
}

func (h *greetHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "greet",
		Description: "Greet a user by name. Returns a friendly greeting message.",
		Parameters: []ParameterDef{
			{
				Name:        "name",
				Type:        "string",
				Required:    true,
				Description: "The name of the person to greet",
			},
		},
		Required: []string{"name"},
	}
}

func (h *greetHandler) Validate(args map[string]any) error {
	name, ok := args["name"].(string)
	if !ok {
		return fmt.Errorf("parameter 'name' must be a string")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("parameter 'name' must not be empty")
	}
	return nil
}

func (h *greetHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()

	// Publish lifecycle events (optional but recommended)
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	name := args["name"].(string)
	output := fmt.Sprintf("Hello, %s! 👋", name)

	return ToolResult{
		Output:  output,
		IsError: false,
	}, nil
}
```

### Step 3: Register the tool in `AllTools()` in `all.go`

Add the handler to the `AllTools()` return slice in `pkg/agent_tools/all.go`:

```go
func AllTools() []ToolHandler {
	return []ToolHandler{
		// ... existing tools ...
		&greetHandler{},   // <-- add your tool here
	}
}
```

---

## The ToolHandler Interface

Every tool must implement this interface:

```go
type ToolHandler interface {
	Name() string
	Definition() ToolDefinition
	Validate(args map[string]any) error
	Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)
}
```

### Method details

#### `Name() string`

Returns the unique tool identifier the LLM uses to call it. This should be a
lowercase, snake_case string (e.g., `"read_file"`, `"shell_command"`, `"greet"`).
The name must be unique across all registered tools — the registry will reject
duplicate registrations.

#### `Definition() ToolDefinition`

Returns the JSON schema the LLM reads to understand what the tool does and what
parameters it accepts:

```go
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  []ParameterDef `json:"parameters"`
	Required    []string       `json:"required,omitempty"`
}

type ParameterDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`        // "string", "integer", "boolean", "array", "object"
	Required    bool   `json:"required"`
	Description string `json:"description"`
}
```

The `Description` field should clearly explain what the tool does, when to use it,
and what it returns. This text is shown to the LLM and directly affects its ability
to use the tool correctly.

#### `Validate(args map[string]any) error`

Runs *before* `Execute()`. Return an error if arguments are invalid. The dual-dispatch
system calls `Validate()` before `Execute()` and wraps any error with context:

```go
if err := handler.Validate(args); err != nil {
    return nil, "", fmt.Errorf("validation failed for tool %q: %w", toolName, err)
}
```

Common validations:
- Type assertions (`args["name"].(string)`)
- Required field presence
- Non-empty string checks
- Path safety (e.g., preventing directory traversal)
- Permission/risk-level checks

Return `nil` if everything is valid.

#### `Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)`

The actual implementation. Returns a `ToolResult` (or an error if the tool crashes).

```go
type ToolResult struct {
	Output        string       `json:"output"`
	StructuredOut any          `json:"structured_out,omitempty"`
	Images        []ImageData  `json:"images,omitempty"`
	TokenUsage    int64        `json:"token_usage"`
	IsError       bool         `json:"is_error"`
}
```

Key points:
- **Output** — The primary text result shown to the LLM
- **StructuredOut** — Optional structured data (maps, slices) for programmatic consumption
- **Images** — For vision-capable tools (e.g., `browse_url` with screenshots)
- **IsError** — `true` means the tool ran but produced an error-state result
- Return a non-nil `error` only for unexpected crashes (panics, I/O failures)

**Publishing events** (recommended):
```go
if env.EventBus != nil {
    env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
        "tool":   toolName,
        "params": args,
    })
    defer func() {
        env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
            "tool":  toolName,
            "error": false,
        })
    }()
}
```

---

## ToolEnv: The Execution Context

`ToolEnv` provides explicit dependencies without coupling tools to the `*Agent` type:

```go
type ToolEnv struct {
	EventBus        *events.EventBus
	WorkspaceRoot   string
	OutputWriter    io.Writer
	ApprovalManager ApprovalManager
	MaxTokensFunc   func() int
	ConfigManager   *configuration.Manager
}
```

| Field | Purpose | Nil-safe? |
|-------|---------|-----------|
| `EventBus` | Publish lifecycle events (`tool_start`, `tool_end`) | Yes — check before use |
| `WorkspaceRoot` | Working directory root for path resolution | Yes — may be empty string `""` if agent is nil |
| `OutputWriter` | Where to write tool output (stdout, logs) | Yes — may be nil in tests or unused by some tools |
| `ApprovalManager` | Request user approval for risky operations | Yes — nil if approvals unsupported (currently always nil pending migration) |
| `MaxTokensFunc` | Returns current token budget limit | Yes — may be nil; check before calling |
| `ConfigManager` | Access configuration (API keys, settings) | Yes — check before use |

The `ApprovalManager` interface:
```go
type ApprovalManager interface {
	RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) ApprovalResult
}

type ApprovalResult struct {
	Approved    bool
	Reason      string
	UserComment string
}
```

---

## Registry & Init Order

### The flow

1. **`all.go` defines `AllTools()`** — Returns a slice of all `ToolHandler` instances,
   one per registered tool. This is the single source of truth for what tools exist.

2. **`GetNewToolRegistry()` returns the global singleton** — Uses `sync.Once` for
   thread-safe lazy initialization. Calls `NewToolRegistry()` internally.

3. **Registration** — During agent startup, tools are registered by iterating
   `AllTools()` and calling `registry.Register(h)` for each handler:

   ```go
   registry := tools.GetNewToolRegistry()
   for _, h := range tools.AllTools() {
       if err := registry.Register(h); err != nil {
           log.Printf("failed to register tool: %v", err)
       }
   }
   ```

4. **Dual-dispatch shim** — The legacy `ExecuteTool()` method in
   `pkg/agent/tool_definitions.go` bridges old and new systems:

   ```go
   func (r *ToolRegistry) ExecuteTool(...) {
       // Check new registry FIRST
       if handler, found := tools.GetNewToolRegistry().Lookup(toolName); found {
           // Build ToolEnv from agent context, call Validate(), Execute()
           // ...
       }
       // Fall back to legacy func-style handlers
       tool, exists := r.tools[toolName]
       // ...
   }
   ```

   This means if a tool exists in the new registry, it takes priority. Legacy
   handlers are only used as a fallback during the migration period.

5. **`ForPersona(persona)`** — Currently returns all registered tools. Per-persona
   filtering is planned for the future (TODO in `registry.go`).

---

## Tools vs. Commands

Sprout has two separate registration systems:

| | **Tools** (`pkg/agent_tools/`) | **Commands** (`pkg/agent_commands/`) |
|---|---|---|
| **Who invokes them** | The AI agent (LLM) | The user (via `/` prefix) |
| **Interface** | `ToolHandler` (Name, Definition, Validate, Execute) | `Command` (Name, Description, Execute) |
| **Signature** | `Execute(ctx, ToolEnv, map[string]any) (ToolResult, error)` | `Execute(args []string, *agent.Agent) error` |
| **Examples** | `read_file`, `shell_command`, `browse_url` | `/help`, `/commit`, `/persona` |
| **Purpose** | Capabilities the AI uses to interact with the world | Direct user commands for configuration and control |

They serve different purposes:
- **Commands** are for the user to configure the agent, trigger workflows, and control behavior.
- **Tools** are capabilities the AI agent can invoke autonomously to read files, run commands,
  search code, delegate to subagents, and more.

Both use a registration pattern with a central registry, but they operate in separate
domains and have different interfaces.

---

## Example: Complete Minimal Tool

Below is a fully self-contained example of a simple tool that greets a user by name.

**File: `pkg/agent_tools/greet_handler.go`**

```go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// greetHandler implements a simple greeting tool.
type greetHandler struct{}

func (h *greetHandler) Name() string {
	return "greet"
}

func (h *greetHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "greet",
		Description: "Greet a user by name. Returns a friendly greeting message.",
		Parameters: []ParameterDef{
			{
				Name:        "name",
				Type:        "string",
				Required:    true,
				Description: "The name of the person to greet",
			},
		},
		Required: []string{"name"},
	}
}

func (h *greetHandler) Validate(args map[string]any) error {
	name, ok := args["name"].(string)
	if !ok {
		return fmt.Errorf("parameter 'name' must be a string")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("parameter 'name' must not be empty")
	}
	return nil
}

func (h *greetHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()

	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	name := args["name"].(string)
	output := fmt.Sprintf("Hello, %s! 👋", name)

	return ToolResult{
		Output:  output,
		IsError: false,
	}, nil
}
```

**Then add `&greetHandler{}` to `AllTools()` in `all.go`.**

---

## Migration Notes

The Sprout codebase is migrating from a legacy `func`-style tool system to this
interface-based approach. Key migration facts:

- **Legacy tools** used `type ToolHandler func(ctx, args, agent) (images, output, error)` —
  a plain function type tightly coupled to `*Agent`.
- **New tools** implement the `ToolHandler` interface, which is decoupled from `*Agent`
  and receives explicit dependencies via `ToolEnv`.
- **Dual-dispatch** in `pkg/agent/tool_definitions.go` checks the new registry first,
  then falls back to legacy handlers. This allows incremental migration without
  breaking existing functionality.
- **Thin wrappers** — Some tools (e.g., `browseURLHandler`, `runSubagentHandler`) are
  temporary thin wrappers around legacy agent methods, pending full refactoring.
  These are marked with comments like "thin wrapper pending *Agent refactoring".
