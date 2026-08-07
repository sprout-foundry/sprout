//go:build !js

package tools

import (
	"context"
	"fmt"
	"strings"
)

// literalFallbackSemantic runs a literal grep when no embedding manager is
// available, so a direct call to semantic_search (from a replayed session or
// saved automation) still returns useful results instead of an empty verdict.
// It reuses the same literal-pattern extraction and search pipeline as the
// `search` tool so conceptual queries are stemmed and OR'd, not passed
// verbatim as a useless regex.
func literalFallbackSemantic(ctx context.Context, env ToolEnv, query string) ToolResult {
	dir, err := resolveSearchDirectory("", env.WorkspaceRoot)
	if err != nil {
		dir = env.WorkspaceRoot
		if dir == "" {
			dir = "."
		}
	}

	limit := 20
	literalRes, literalErr := runLiteralSearch(ctx, literalSearchOpts{
		Directory:  dir,
		Pattern:    literalPatternFor(query),
		MaxFiles:   limit * 5,
		MaxPerFile: 3,
	})

	if literalErr != nil && len(literalRes.Hits) == 0 {
		return ToolResult{
			Output: fmt.Sprintf("No results found matching: %q\n\nThe embedding index is not available and the literal search could not complete: %v", query, literalErr),
		}
	}

	if len(literalRes.Hits) == 0 {
		return ToolResult{
			Output: fmt.Sprintf("No results found matching: %q.\n\nThe embedding index is not available, so only exact text matches were considered. Try the `search` or `search_files` tool for broader matching.", query),
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d literal match(es) for %q (semantic search unavailable — embedding index not enabled):\n\n", len(literalRes.Hits), query))
	for _, h := range literalRes.Hits {
		path := normalizeSearchPath(h.Path, env.WorkspaceRoot)
		sb.WriteString(fmt.Sprintf("%s:%d\n", path, h.Line))
		if t := strings.TrimSpace(h.Text); t != "" {
			if len(t) > 200 {
				t = t[:200] + "…"
			}
			sb.WriteString("    " + t + "\n")
		}
	}
	sb.WriteString("\nUse `read_file` to view the full content of any result.")

	return ToolResult{
		Output:     sb.String(),
		TokenUsage: int64(estimateTokenUsage(sb.String())),
	}
}
