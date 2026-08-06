//go:build !js

package tools

// registerSearchTool adds the fused `search` tool, which merges a literal
// filesystem walk with semantic index results. Split from all.go so the WASM
// (GOOS=js) build doesn't try to compile the filesystem walker — the same
// reason registerCodegraphTools is split out.
func registerSearchTool() []ToolHandler {
	return []ToolHandler{
		&searchHandler{},
	}
}
