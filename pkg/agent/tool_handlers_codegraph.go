package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// openCodegraphStore creates a new codegraph store instance at the default path.
// Returns nil, nil if the database file doesn't exist yet (so handlers can
// return a friendly "not indexed" message).
func openCodegraphStore() (*codegraph.SQLiteStore, error) {
	gitRoot, err := git.GetGitRootDir()
	if err != nil {
		return nil, nil // No git root, can't have index
	}
	dbPath := filepath.Join(gitRoot, ".sprout", "codegraph.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil // DB doesn't exist, not indexed yet
	}
	return codegraph.NewStore("") // Opens default path
}

// formatSymbolList formats a list of symbols into human-readable lines.
// Each line looks like: "  - func pkg/Foo.Bar (pkg/foo.go:42)"
func formatSymbolList(symbols []codegraph.Symbol) string {
	var b strings.Builder
	for _, sym := range symbols {
		fmt.Fprintf(&b, "  - %s %s (%s:%d)\n", sym.Kind, sym.QualifiedName, sym.FilePath, sym.Line)
	}
	return b.String()
}

// handleGetCallers queries the code intelligence graph for callers of a symbol.
func handleGetCallers(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	qualifiedName, ok := args["qualified_name"].(string)
	if !ok || qualifiedName == "" {
		return "", agenterrors.NewTool("get_callers", "qualified_name must be a non-empty string", nil)
	}

	store, err := openCodegraphStore()
	if err != nil {
		return "", agenterrors.NewTool("get_callers", "open store", err)
	}
	if store == nil {
		return "The code intelligence graph has not been indexed yet. Run indexing first (e.g., `sprout index` or the equivalent agent workflow).", nil
	}
	defer store.Close()

	callers, err := store.QueryCallers(ctx, qualifiedName)
	if err != nil {
		return "", agenterrors.NewTool("get_callers", "query callers", err)
	}

	if len(callers) == 0 {
		return fmt.Sprintf("No callers found for '%s'.", qualifiedName), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Callers of %s (%d):\n", qualifiedName, len(callers))
	b.WriteString(formatSymbolList(callers))
	return b.String(), nil
}

// handleGetCallees queries the code intelligence graph for callees of a symbol.
func handleGetCallees(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	qualifiedName, ok := args["qualified_name"].(string)
	if !ok || qualifiedName == "" {
		return "", agenterrors.NewTool("get_callees", "qualified_name must be a non-empty string", nil)
	}

	store, err := openCodegraphStore()
	if err != nil {
		return "", agenterrors.NewTool("get_callees", "open store", err)
	}
	if store == nil {
		return "The code intelligence graph has not been indexed yet. Run indexing first (e.g., `sprout index` or the equivalent agent workflow).", nil
	}
	defer store.Close()

	callees, err := store.QueryCallees(ctx, qualifiedName)
	if err != nil {
		return "", agenterrors.NewTool("get_callees", "query callees", err)
	}

	if len(callees) == 0 {
		return fmt.Sprintf("No callees found for '%s'.", qualifiedName), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Callees of %s (%d):\n", qualifiedName, len(callees))
	b.WriteString(formatSymbolList(callees))
	return b.String(), nil
}

// handleFindDeadCode queries the code intelligence graph for dead code.
// The optional directory parameter restricts results to files under that prefix.
func handleFindDeadCode(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	dir, _ := args["directory"].(string)

	store, err := openCodegraphStore()
	if err != nil {
		return "", agenterrors.NewTool("find_dead_code", "open store", err)
	}
	if store == nil {
		return "The code intelligence graph has not been indexed yet. Run indexing first (e.g., `sprout index` or the equivalent agent workflow).", nil
	}
	defer store.Close()

	deadCode, err := store.FindDeadCode(ctx, dir)
	if err != nil {
		return "", agenterrors.NewTool("find_dead_code", "find dead code", err)
	}

	if len(deadCode) == 0 {
		return "No dead code found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Dead code found (%d):\n", len(deadCode))
	b.WriteString(formatSymbolList(deadCode))
	return b.String(), nil
}
