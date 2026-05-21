package tools

// AllTools returns all available tool handlers for registration.
// Initially returns an empty slice — tools will be added as they migrate.
// This is the central registration point for the new interface-based tool system.
//
// To register all tools with a registry:
//
//	registry := tools.NewToolRegistry()
//	for _, h := range tools.AllTools() {
//	    registry.Register(h)
//	}
func AllTools() []ToolHandler {
	return []ToolHandler{}
}
