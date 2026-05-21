package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the new interface-based tool system.
// Currently includes: read_file, list_directory, and fetch_url.
//
// To register all tools with a registry:
//
//	registry := tools.NewToolRegistry()
//	for _, h := range tools.AllTools() {
//	    registry.Register(h)
//	}
func AllTools() []ToolHandler {
	return []ToolHandler{
		&readFileHandler{},
		&listDirHandler{},
		&fetchURLHandler{},
	}
}
