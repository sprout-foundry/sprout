//go:build js

package tools

// WASM build: codegraph not available (requires CGo/SQLite).
func registerCodegraphTools() []ToolHandler {
	return nil
}
