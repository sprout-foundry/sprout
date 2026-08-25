package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// repoMapSemanticTopK bounds how many semantically-matched files widen the map.
const repoMapSemanticTopK = 40

type repoMapHandler struct{}

func (h *repoMapHandler) Name() string {
	return "repo_map"
}

func (h *repoMapHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "repo_map",
		Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task. Supports Go, TypeScript, JavaScript, Python, Rust, Java, and C files.", Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Directory to scan (default: .)"},
			{Name: "depth", Type: "integer", Description: "Detail level: 1=directory tree only, 2=tree+top-level symbols, 3=full symbols (default)"},
			{Name: "query", Type: "string", Description: "Filter the map to what is relevant. Matches file paths and symbol names as a case-insensitive substring, and — when the embedding index is enabled — also files that match the query semantically, so a conceptual query like 'user authentication' works without knowing the identifier."},
		},
		Required: []string{},
	}
}

func (h *repoMapHandler) Validate(args map[string]any) error {
	return nil
}

func (h *repoMapHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	directory, _ := extractString(args, "directory")
	if directory == "" {
		directory = "."
	}

	// Gate 1 precheck — only when the caller named a non-default directory.
	// The default "." resolves to the workspace root, which is already
	// allowlisted; gating it would prompt on every vanilla repo_map call.
	if directory != "." {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "repo_map", directory)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", directory), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", directory)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "repo_map", directory, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", directory), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", directory)
			}
		}
	}

	depth := env.RepoMapDefaultDepth // profile override (1 in LCM, 0 = default 3)
	if depth <= 0 {
		depth = 3 // default: full symbols
	}
	if d, ok := args["depth"]; ok {
		switch v := d.(type) {
		case int:
			depth = v
		case int64:
			depth = int(v)
		case float64:
			depth = int(v)
		}
	}

	query, _ := extractString(args, "query")

	output, err := GenerateRepoMapWithSemanticMatches(ctx, directory, depth, query,
		semanticMatchesForQuery(ctx, env, directory, query))
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error generating repo map: %v", err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output:  output,
		IsError: false,
	}, nil
}

func (h *repoMapHandler) Aliases() []string      { return nil }
func (h *repoMapHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *repoMapHandler) MaxResultSize() int     { return 0 }
func (h *repoMapHandler) SafeForParallel() bool  { return false }
func (h *repoMapHandler) Interactive() bool      { return false }

// semanticMatchesForQuery resolves the workspace-relative files that a semantic
// search associates with query, or nil when semantic search is unavailable.
// Every failure mode degrades to plain substring behavior.
func semanticMatchesForQuery(ctx context.Context, env ToolEnv, directory, query string) map[string]bool {
	// An index with no records cannot contribute matches, and querying it costs
	// a ~145ms embed for a guaranteed-empty result.
	if query == "" || env.EmbeddingMgr == nil || !env.EmbeddingMgr.Readiness().CanAnswerQueries() {
		return nil
	}

	root := env.WorkspaceRoot
	if root == "" {
		root = directory
	}

	// Bounded: this runs on a tool call the user is waiting on.
	results, err := env.EmbeddingMgr.QuerySimilar(ctx, query, repoMapSemanticTopK,
		env.EmbeddingMgr.SemanticSearchThreshold())
	if err != nil || len(results) == 0 {
		return nil
	}

	// Records store whatever path shape the index build used — absolute in the
	// daemon, relative when built from a relative root. Both sides have to be
	// absolutised before Rel, or the result keeps a "../.." prefix and never
	// matches the walk's workspace-relative paths.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil
	}

	paths := make(map[string]bool, len(results))
	for _, r := range results {
		abs, err := filepath.Abs(r.Record.File)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // outside the workspace being mapped
		}
		paths[filepath.ToSlash(rel)] = true
	}
	return paths
}
