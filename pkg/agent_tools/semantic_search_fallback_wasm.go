//go:build js

package tools

import (
	"context"
	"fmt"
)

// literalFallbackSemantic is the WASM stub. The literal search pipeline
// (runLiteralSearch, literalPatternFor) is excluded from WASM builds, so the
// fallback returns an honest "no results" verdict instead. This branch is only
// reached on direct calls from replayed sessions; the tool is hidden from the
// model when embeddings are off.
func literalFallbackSemantic(_ context.Context, _ ToolEnv, query string) ToolResult {
	return ToolResult{
		Output: fmt.Sprintf("No results found matching: %q.\n\nThe embedding index is not available, so only exact text matches were considered. Try the `search` or `search_files` tool for broader matching.", query),
	}
}
