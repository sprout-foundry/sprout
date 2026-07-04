package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// --- get_callers ---

type getCallersHandler struct{}

func (h *getCallersHandler) Name() string { return "get_callers" }

func (h *getCallersHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "get_callers",
		Description: "Find all functions that call the given function. Requires the code intelligence graph to be indexed. Input: qualified_name (e.g. 'pkg/agent.Agent.ProcessQuery'). Returns a list of callers with file:line locations.",
		Required:    []string{"qualified_name"},
		Parameters: []ParameterDef{
			{Name: "qualified_name", Type: "string", Description: "The qualified name of the function to find callers for (e.g. 'pkg/agent.Agent.ClearConversationHistory').", Required: true},
		},
	}
}

func (h *getCallersHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "qualified_name")
}

func (h *getCallersHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	qn, _ := args["qualified_name"].(string)
	if qn == "" {
		return ToolResult{Output: "qualified_name is required", IsError: true}, nil
	}

	store, err := openCodegraphStoreHandler()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to open code graph store: %v", err), IsError: true}, nil
	}
	if store == nil {
		return ToolResult{Output: "The code intelligence graph has not been indexed yet."}, nil
	}
	defer store.Close()

	callers, err := store.QueryCallers(ctx, qn)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Query failed: %v", err), IsError: true}, nil
	}

	if len(callers) == 0 {
		return ToolResult{Output: fmt.Sprintf("No callers found for '%s'.", qn)}, nil
	}

	return ToolResult{Output: formatSymbolListHandler("Callers of "+qn, callers)}, nil
}

func (h *getCallersHandler) Aliases() []string      { return nil }
func (h *getCallersHandler) Timeout() time.Duration { return 10 * time.Second }
func (h *getCallersHandler) MaxResultSize() int     { return 0 }
func (h *getCallersHandler) SafeForParallel() bool  { return true }
func (h *getCallersHandler) Interactive() bool      { return false }

// --- get_callees ---

type getCalleesHandler struct{}

func (h *getCalleesHandler) Name() string { return "get_callees" }

func (h *getCalleesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "get_callees",
		Description: "Find all functions called by the given function. Requires the code intelligence graph to be indexed. Input: qualified_name. Returns a list of callees with file:line locations.",
		Required:    []string{"qualified_name"},
		Parameters: []ParameterDef{
			{Name: "qualified_name", Type: "string", Description: "The qualified name of the function to find callees for.", Required: true},
		},
	}
}

func (h *getCalleesHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "qualified_name")
}

func (h *getCalleesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	qn, _ := args["qualified_name"].(string)
	if qn == "" {
		return ToolResult{Output: "qualified_name is required", IsError: true}, nil
	}

	store, err := openCodegraphStoreHandler()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to open code graph store: %v", err), IsError: true}, nil
	}
	if store == nil {
		return ToolResult{Output: "The code intelligence graph has not been indexed yet."}, nil
	}
	defer store.Close()

	callees, err := store.QueryCallees(ctx, qn)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Query failed: %v", err), IsError: true}, nil
	}

	if len(callees) == 0 {
		return ToolResult{Output: fmt.Sprintf("No callees found for '%s'.", qn)}, nil
	}

	return ToolResult{Output: formatSymbolListHandler("Callees of "+qn, callees)}, nil
}

func (h *getCalleesHandler) Aliases() []string      { return nil }
func (h *getCalleesHandler) Timeout() time.Duration { return 10 * time.Second }
func (h *getCalleesHandler) MaxResultSize() int     { return 0 }
func (h *getCalleesHandler) SafeForParallel() bool  { return true }
func (h *getCalleesHandler) Interactive() bool      { return false }

// --- find_dead_code ---

type findDeadCodeHandler struct{}

func (h *findDeadCodeHandler) Name() string { return "find_dead_code" }

func (h *findDeadCodeHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "find_dead_code",
		Description: "Find functions with zero inbound call edges (dead code). Excludes entry points like main(), init(), exported API, and test functions. Requires the code intelligence graph to be indexed.",
		Required:    []string{},
		Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Optional: restrict search to a specific directory."},
		},
	}
}

func (h *findDeadCodeHandler) Validate(args map[string]any) error { return nil }

func (h *findDeadCodeHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	store, err := openCodegraphStoreHandler()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to open code graph store: %v", err), IsError: true}, nil
	}
	if store == nil {
		return ToolResult{Output: "The code intelligence graph has not been indexed yet."}, nil
	}
	defer store.Close()

	dir, _ := args["directory"].(string)
	dead, err := store.FindDeadCode(ctx, dir)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Query failed: %v", err), IsError: true}, nil
	}

	if len(dead) == 0 {
		return ToolResult{Output: "No dead code found."}, nil
	}

	return ToolResult{Output: formatSymbolListHandler(fmt.Sprintf("Dead code found (%d)", len(dead)), dead)}, nil
}

func (h *findDeadCodeHandler) Aliases() []string      { return nil }
func (h *findDeadCodeHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *findDeadCodeHandler) MaxResultSize() int     { return 0 }
func (h *findDeadCodeHandler) SafeForParallel() bool  { return true }
func (h *findDeadCodeHandler) Interactive() bool      { return false }

// --- helpers ---

func openCodegraphStoreHandler() (*codegraph.SQLiteStore, error) {
	gitRoot, err := git.GetGitRootDir()
	if err != nil {
		return nil, nil
	}
	dbPath := filepath.Join(gitRoot, ".sprout", "codegraph.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	return codegraph.NewStore("")
}

func formatSymbolListHandler(title string, symbols []codegraph.Symbol) string {
	var b strings.Builder
	b.WriteString(title + ":\n")
	for _, sym := range symbols {
		fmt.Fprintf(&b, "  - %s %s (%s:%d)\n", sym.Kind, sym.QualifiedName, sym.FilePath, sym.Line)
	}
	return b.String()
}

// requireArgs validates that the given required args are present.
func requireArgs(toolName string, args map[string]any, required ...string) error {
	for _, key := range required {
		v, ok := args[key]
		if !ok {
			return fmt.Errorf("%s: missing required parameter %q", toolName, key)
		}
		if s, ok := v.(string); ok && s == "" {
			return fmt.Errorf("%s: parameter %q must not be empty", toolName, key)
		}
	}
	return nil
}
