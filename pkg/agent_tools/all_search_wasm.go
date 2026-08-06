//go:build js

package tools

// WASM build: the fused search tool is unavailable — its literal pass walks the
// real filesystem. semantic_search remains registered for browser sessions that
// have a JS-side embedding provider.
func registerSearchTool() []ToolHandler {
	return nil
}
