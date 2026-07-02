package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the new interface-based tool system.
// Currently includes: read_file, list_directory, fetch_url, search_files,
// repo_map, rollback_changes, view_history,
// list_skills, embedding_index, write_file, write_structured_file,
// edit_file, shell_command, save_memory, search_memories,
// task_queue_add, task_queue_publish,
// task_queue_read, todo_write, todo_read, ask_user, patch_structured_file,
// commit, git, activate_skill,
// browse_url, web_search, semantic_search, analyze_image_content,
// and analyze_ui_screenshot.
//
// Memory operations (add/read/list/delete/search) are exposed as the
// consolidated manage_memory tool registered in
// pkg/agent/tool_registrations.go. The legacy add_memory / read_memory /
// list_memories / delete_memory handlers were removed once manage_memory
// covered the full surface.
//
// Subagent tools (run_subagent / run_parallel_subagents) are NOT in this
// list — they live exclusively in the seed registry under pkg/agent
// because they require *Agent access for nested runner orchestration.
// SP-059 Phase 3b removed earlier stub entries that returned hardcoded
// errors; the seed registry's dual-dispatch path is canonical.
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
		&rollbackChangesHandler{},
		&viewHistoryHandler{},
		&listSkillsHandler{},
		&embeddingIndexHandler{},
		&writeFileHandler{},
		&writeStructuredFileHandler{},
		&editFileHandler{},
		&shellCommandHandler{},
		&saveMemoryHandler{},
		&searchMemoriesHandler{},
		// Subagent tools live in the seed registry (pkg/agent); see
		// the package-level comment above for context.
		// Task queue tools
		&taskQueueAddHandler{},
		&taskQueuePublishHandler{},
		&taskQueueReadHandler{},
		// Todo tools
		&todoWriteHandler{},
		&todoReadHandler{},
		// Interaction tools
		&askUserHandler{},
		// Structured file tools
		&patchStructuredFileHandler{},
		// Git tools
		&commitHandler{},
		&gitHandler{},
		// Skill tools
		&activateSkillHandler{},
		// Browser/search tools
		&browseURLHandler{},
		&webSearchHandler{},
		&semanticSearchHandler{},
		// Image/analysis tools
		&analyzeImageContentHandler{},
		&analyzeUIScreenshotHandler{},
	}
}
