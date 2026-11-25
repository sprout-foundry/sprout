// tools provides a minimal tool executor with a small set of example tools.
//
// This is an EXAMPLE only. A real implementation would need:
//
//   - Path traversal prevention (reject ../ or absolute paths escaping the workspace)
//   - File size limits (cap reads and writes to prevent memory exhaustion)
//   - File type allowlisting (prevent writing executables, symlinks, or device files)
//   - Shell command sandboxing (restrict allowed commands, deny network access, limit resources)
//   - Argument sanitization (reject null bytes, extremely long strings, etc.)
//   - Rate limiting and quota enforcement per user/session
//   - Audit logging of all tool invocations
//   - Circuit breakers to prevent cascading failures
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/seed/core"
)

// maxContent caps tool output to prevent runaway responses.
// REAL: Per-tool limits make more sense — e.g. 100KB for reads, 500KB for shell.
const maxContent = 100 * 1024

// toolExec implements a minimal ToolExecutor with basic file and shell tools.
type toolExec struct {
	wd string // working directory — all paths are relative to this
}

func newToolExec(wd string) *toolExec {
	return &toolExec{wd: wd}
}

// GetTools returns the list of available tools.
//
// REAL: Tools should be registered dynamically via a registry, not hardcoded.
// Each tool should have its own security policy, rate limit, and quota.
func (e *toolExec) GetTools() []core.Tool {
	return []core.Tool{
		{
			Type: "function",
			Function: core.ToolFunction{
				Name:        "read_file",
				Description: "Read a file and return its contents. Path is relative to the working directory.",
				Parameters: core.ToolParameters{
					Type: "object",
					Properties: map[string]core.ToolParameter{
						"path": {Type: "string", Description: "File path relative to working directory"},
					},
					Required: []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: core.ToolFunction{
				Name:        "write_file",
				Description: "Write content to a file. Path is relative to the working directory. Creates parent directories as needed.",
				Parameters: core.ToolParameters{
					Type: "object",
					Properties: map[string]core.ToolParameter{
						"path":    {Type: "string", Description: "File path relative to working directory"},
						"content": {Type: "string", Description: "Content to write"},
					},
					Required: []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: core.ToolFunction{
				Name:        "shell_command",
				Description: "Execute a shell command in the working directory. Limited to simple, read-only operations.",
				Parameters: core.ToolParameters{
					Type: "object",
					Properties: map[string]core.ToolParameter{
						"command": {Type: "string", Description: "Shell command to execute"},
					},
					Required: []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: core.ToolFunction{
				Name:        "list_directory",
				Description: "List files in a directory. Path is relative to the working directory.",
				Parameters: core.ToolParameters{
					Type: "object",
					Properties: map[string]core.ToolParameter{
						"path": {Type: "string", Description: "Directory path relative to working directory"},
					},
					Required: []string{"path"},
				},
			},
		},
	}
}

// Execute runs the given tool calls and returns result messages.
func (e *toolExec) Execute(_ context.Context, calls []core.ToolCall) []core.Message {
	msgs := make([]core.Message, len(calls))
	for i, c := range calls {
		content := e.runTool(c.Function.Name, c.Function.Arguments)
		msgs[i] = core.Message{Role: "tool", ToolCallID: c.ID, Content: content}
	}
	return msgs
}

func (e *toolExec) runTool(name, argsJSON string) string {
	switch name {
	case "read_file":
		return e.readFile(argsJSON)
	case "write_file":
		return e.writeFile(argsJSON)
	case "shell_command":
		return e.shellCommand(argsJSON)
	case "list_directory":
		return e.listDirectory(argsJSON)
	default:
		return fmt.Sprintf("Error: unknown tool '%s'", name)
	}
}

// ---------------------------------------------------------------------------
// read_file
//
// REAL IMPLEMENTATION WOULD:
//   - Resolve the path and ensure it stays within the working directory
//   - Check file size before reading (e.g. reject files > 1MB)
//   - Reject symlinks that point outside the working directory
//   - Reject device files, FIFOs, sockets
//   - Reject hidden/system files (.git/credentials, .env, etc.)
// ---------------------------------------------------------------------------

func (e *toolExec) readFile(raw string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil || args.Path == "" {
		return "Error: path is required"
	}

	// REAL: Validate the resolved path stays within e.wd.
	//   abs, err := filepath.Abs(filepath.Join(e.wd, args.Path))
	//   if !strings.HasPrefix(abs, wdAbs) { return "Error: path outside workspace" }

	fp := filepath.Join(e.wd, args.Path)
	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return trunc(string(data))
}

// ---------------------------------------------------------------------------
// write_file
//
// REAL IMPLEMENTATION WOULD:
//   - Resolve the path and ensure it stays within the working directory
//   - Cap content size (e.g. 1MB max) to prevent disk exhaustion
//   - Reject writing to sensitive paths (.git/*, .env, etc.)
//   - Require explicit confirmation for overwriting existing files
//   - Audit log all writes
// ---------------------------------------------------------------------------

func (e *toolExec) writeFile(raw string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil || args.Path == "" {
		return "Error: path and content are required"
	}

	// REAL: Cap content size before writing.
	//   if len(args.Content) > 1*1024*1024 { return "Error: content exceeds 1MB limit" }

	fp := filepath.Join(e.wd, args.Path)
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		return fmt.Sprintf("Error creating directory: %v", err)
	}

	if err := os.WriteFile(fp, []byte(args.Content), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), args.Path)
}

// ---------------------------------------------------------------------------
// shell_command
//
// REAL IMPLEMENTATION WOULD:
//   - Whitelist allowed commands (e.g. cat, head, grep, ls, git status)
//   - Deny dangerous commands (rm, chmod, curl, wget, ssh, sudo, etc.)
//   - Run in a restricted environment (no network, limited CPU/memory)
//   - Enforce a short timeout (e.g. 10s) to prevent hanging
//   - Sanitize arguments to prevent shell injection
//   - Run in a chroot or container for isolation
//   - Audit log all command executions
// ---------------------------------------------------------------------------

func (e *toolExec) shellCommand(raw string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil || args.Command == "" {
		return "Error: command is required"
	}

	// REAL: Check against a whitelist of allowed commands before executing.
	//   cmdName := strings.Fields(args.Command)[0]
	//   if !allowedCommands[cmdName] { return "Error: command not allowed" }

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", args.Command)
	cmd.Dir = e.wd
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil {
		s += fmt.Sprintf("\nError: %v", err)
	}
	return trunc(s)
}

// ---------------------------------------------------------------------------
// list_directory
//
// REAL IMPLEMENTATION WOULD:
//   - Validate path stays within working directory
//   - Filter out hidden/system files by default
//   - Cap the number of entries returned
// ---------------------------------------------------------------------------

func (e *toolExec) listDirectory(raw string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil || args.Path == "" {
		return "Error: path is required"
	}

	fp := filepath.Join(e.wd, args.Path)
	entries, err := os.ReadDir(fp)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Directory: %s", args.Path))
	for _, entry := range entries {
		info, _ := entry.Info()
		sym := "-"
		if entry.IsDir() {
			sym = "d"
		}
		lines = append(lines, fmt.Sprintf("  %s  %10d  %s", sym, info.Size(), entry.Name()))
	}
	return trunc(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// trunc — cap output to prevent runaway tool responses
// ---------------------------------------------------------------------------

func trunc(s string) string {
	if len(s) > maxContent {
		return s[:maxContent] + "\n[...truncated...]"
	}
	return s
}
