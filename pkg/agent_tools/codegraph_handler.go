//go:build !js

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// codegraphBuildMu guards codegraphBuildAttempted to ensure only one
// background build runs at a time while still allowing retry on failure.
var (
	codegraphBuildMu        sync.Mutex
	codegraphBuildAttempted bool
)

// triggerCodegraphBuild kicks off a background goroutine that populates
// the codegraph database. Already-populated DBs are skipped.
// A failed build clears the attempted flag so the next call retries.
func triggerCodegraphBuild() {
	codegraphBuildMu.Lock()
	if codegraphBuildAttempted {
		codegraphBuildMu.Unlock()
		return
	}
	codegraphBuildAttempted = true
	codegraphBuildMu.Unlock()

	go func() {
		store, err := codegraph.NewStore("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "codegraph auto-build: failed to open store: %v\n", err)
			allowCodegraphRetry()
			return
		}
		defer store.Close()

		// Skip if already populated (another process/goroutine may have built it).
		if store.Stats().FileCount > 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := store.IndexAll(ctx, codegraphFileParser); err != nil {
			fmt.Fprintf(os.Stderr, "codegraph auto-build failed: %v (will retry on next query tool use)\n", err)
			if dbPath, pathErr := codegraph.DefaultDBPath(); pathErr == nil {
				_ = os.Remove(dbPath)
			}
			allowCodegraphRetry()
			return
		}

		stats := store.Stats()
		fmt.Fprintf(os.Stderr, "codegraph auto-build complete: %d nodes, %d edges, %d files\n",
			stats.NodeCount, stats.EdgeCount, stats.FileCount)
	}()
}

// codegraphRefreshMu guards a single in-flight incremental refresh.
// codegraphRefreshAt rate-limits how often one is started.
var (
	codegraphRefreshMu      sync.Mutex
	codegraphRefreshRunning bool
	codegraphRefreshAt      time.Time
)

// codegraphRefreshInterval is how long a refresh is considered fresh enough.
const codegraphRefreshInterval = 30 * time.Second

// triggerCodegraphRefresh re-indexes files whose mtime is newer than their
// last_indexed timestamp, in the background. Failures are logged but never
// block the query the caller asked for.
func triggerCodegraphRefresh() {
	codegraphRefreshMu.Lock()
	if codegraphRefreshRunning || time.Since(codegraphRefreshAt) < codegraphRefreshInterval {
		codegraphRefreshMu.Unlock()
		return
	}
	codegraphRefreshRunning = true
	codegraphRefreshAt = time.Now()
	codegraphRefreshMu.Unlock()

	go func() {
		defer func() {
			codegraphRefreshMu.Lock()
			codegraphRefreshRunning = false
			codegraphRefreshMu.Unlock()
		}()

		store, err := codegraph.NewStore("")
		if err != nil {
			return
		}
		defer store.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := store.IndexChangedFiles(ctx, codegraphFileParser); err != nil {
			fmt.Fprintf(os.Stderr, "codegraph refresh failed: %v (queries will use the previous graph)\n", err)
		}
	}()
}

// allowCodegraphRetry clears the attempted flag so the next query tool call
// can trigger a fresh background build.
func allowCodegraphRetry() {
	codegraphBuildMu.Lock()
	codegraphBuildAttempted = false
	codegraphBuildMu.Unlock()
}

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
		Description: "Find functions with zero inbound call edges (dead code candidates). Results are grouped by confidence tier: high (likely dead), medium (possibly dead), low (probably alive via dynamic dispatch). Excludes entry points like main(), init(), exported API, and test functions. Requires the code intelligence graph to be indexed. Results are candidates for manual review, not authoritative dead code — static analysis cannot trace reflection, interface dispatch, or command-registration closures.",
		Required:    []string{},
		Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Optional: restrict search to a specific directory."},
		},
	}
}

func (h *findDeadCodeHandler) Validate(args map[string]any) error { return nil }

func (h *findDeadCodeHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	dir, _ := args["directory"].(string)

	// Gate 1 precheck — only when the caller named a directory.
	if dir != "" {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "find_dead_code", dir)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", dir), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", dir)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "find_dead_code", dir, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", dir), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", dir)
			}
		}
	}

	store, err := openCodegraphStoreHandler()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to open code graph store: %v", err), IsError: true}, nil
	}
	if store == nil {
		return ToolResult{Output: "The code intelligence graph has not been indexed yet."}, nil
	}
	defer store.Close()

	candidates, err := store.FindDeadCodeWithMeta(ctx, dir)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Query failed: %v", err), IsError: true}, nil
	}

	if len(candidates) == 0 {
		return ToolResult{Output: "No dead code found."}, nil
	}

	return ToolResult{Output: formatDeadCodeWithConfidence(candidates)}, nil
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
		// Trigger a background build so that subsequent calls have data.
		triggerCodegraphBuild()
		return nil, nil
	}
	// The store exists but may be stale. Refresh it in the background.
	triggerCodegraphRefresh()
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

func formatDeadCodeWithConfidence(candidates []codegraph.DeadCodeCandidate) string {
	groups := map[codegraph.ConfidenceLevel][]codegraph.DeadCodeCandidate{
		codegraph.ConfidenceHigh:   {},
		codegraph.ConfidenceMedium: {},
		codegraph.ConfidenceLow:    {},
	}

	for _, c := range candidates {
		groups[c.Confidence] = append(groups[c.Confidence], c)
	}

	var b strings.Builder
	totalHigh := len(groups[codegraph.ConfidenceHigh])
	totalMedium := len(groups[codegraph.ConfidenceMedium])
	totalLow := len(groups[codegraph.ConfidenceLow])
	fmt.Fprintf(&b, "Dead code candidates (%d total)\n\n", len(candidates))

	b.WriteString("══════════════════════════════════════════════\n")
	fmt.Fprintf(&b, "HIGH confidence (%d) — very likely dead\n", totalHigh)
	b.WriteString("══════════════════════════════════════════════\n")
	appendConfidenceGroup(&b, groups[codegraph.ConfidenceHigh])

	fmt.Fprintf(&b, "══════════════════════════════════════════════\n")
	fmt.Fprintf(&b, "MEDIUM confidence (%d) — possibly dead\n", totalMedium)
	b.WriteString("══════════════════════════════════════════════\n")
	if totalMedium > 0 {
		fmt.Fprintf(&b, "  In handler/registration files or names. Verify before deleting.\n")
		appendConfidenceGroup(&b, groups[codegraph.ConfidenceMedium])
	} else {
		b.WriteString("  (none)\n")
	}

	fmt.Fprintf(&b, "══════════════════════════════════════════════\n")
	fmt.Fprintf(&b, "LOW confidence (%d) — probably alive via dynamic dispatch\n", totalLow)
	b.WriteString("══════════════════════════════════════════════\n")
	fmt.Fprintf(&b, "  Likely false positives: wired via closures, maps, JSX, or reflection.\n")
	if totalLow == 0 {
		b.WriteString("  (none)\n")
	} else if totalLow <= 20 {
		appendConfidenceGroup(&b, groups[codegraph.ConfidenceLow])
	} else {
		fmt.Fprintf(&b, "  (%d candidates — suppressed; use --directory to narrow scope)\n", totalLow)
	}

	b.WriteString("\nTip: Use get_callers to verify a HIGH-confidence candidate before deleting.\n")
	return b.String()
}

func appendConfidenceGroup(b *strings.Builder, candidates []codegraph.DeadCodeCandidate) {
	for _, c := range candidates {
		annotation := ""
		if c.TestCallers > 0 {
			annotation = fmt.Sprintf(" [test-only: %d caller(s)]", c.TestCallers)
		}
		fmt.Fprintf(b, "  - %s %s (%s:%d)%s\n",
			c.Symbol.Kind, c.Symbol.QualifiedName, c.Symbol.FilePath, c.Symbol.Line, annotation)
	}
}

// requireArgs validates that the given required args are present.
func requireArgs(toolName string, args map[string]any, required ...string) error {
	for _, key := range required {
		v, ok := args[key]
		if !ok {
			return agenterrors.NewValidation(fmt.Sprintf("%s: missing required parameter %q", toolName, key), nil)
		}
		if s, ok := v.(string); ok && s == "" {
			return agenterrors.NewValidation(fmt.Sprintf("%s: parameter %q must not be empty", toolName, key), nil)
		}
	}
	return nil
}
