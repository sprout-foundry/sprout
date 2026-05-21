package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the new interface-based tool system.
// Currently includes: read_file, list_directory, fetch_url, search_files,
// repo_map, list_memories, read_memory, rollback_changes, view_history,
// list_skills, and embedding_index.
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
		&searchFilesHandler{},
		&repoMapHandler{},
		&listMemoriesHandler{},
		&readMemoryHandler{},
		&rollbackChangesHandler{},
		&viewHistoryHandler{},
		&listSkillsHandler{},
		&embeddingIndexHandler{},
	}
}
