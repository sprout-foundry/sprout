// Package tools provides the ToolHandler interface, ToolRegistry, and individual
// tool implementations for the Sprout agent. As tools are migrated from the
// legacy switch-based dispatch (pkg/agent/tool_executor*.go) to the registry
// pattern, they register themselves via an init function in this file or in
// their own source files.
//
// SP-038: This file serves as the central tools-init entry point. Over time,
// migrated tools will be imported here so that a single import of this package
// ensures all new-style tools are registered.
package tools

// RegisterAllTools registers all migrated tool handlers into the given registry.
// Call this once at startup to populate the registry with all new-style tools.
func RegisterAllTools(registry *ToolRegistry) {
	registry.Register(NewReadFileHandler())
	registry.Register(NewListDirectoryHandler())
	registry.Register(NewFetchURLHandler())
}
