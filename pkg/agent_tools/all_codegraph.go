//go:build !js

package tools

// registerCodegraphTools adds the code intelligence graph tools that
// depend on CGo/SQLite. Split from all.go so the WASM (GOOS=js) build
// doesn't try to compile the codegraph dependency.
func registerCodegraphTools() []ToolHandler {
	return []ToolHandler{
		&getCallersHandler{},
		&getCalleesHandler{},
		&findDeadCodeHandler{},
	}
}
