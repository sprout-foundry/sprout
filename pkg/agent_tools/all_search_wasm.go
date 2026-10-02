//go:build js

package tools

// WASM build: the search tool is unavailable — its literal pass walks the real
// filesystem.
func registerSearchTool() []ToolHandler {
	return nil
}
