package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the new interface-based tool system.
// Currently includes: read_file, list_directory, fetch_url, search_files,
// repo_map, rollback_changes, view_history, list_skills, embedding_index,
// write_file, write_structured_file, edit_file, shell_command,
// manage_memory, manage_settings, todo_write, todo_read,
// ask_user, patch_structured_file, commit, git, activate_skill,
// web_search, semantic_search, analyze_image_content,
// analyze_ui_screenshot, list_automate_workflows, list_changes,
// revert_my_changes, recover_file, create_pull_request, run_automate,
// mcp_refresh, run_subagent, run_parallel_subagents,
// request_clarification, and respond_clarification.
//
// browse_url is registered conditionally via registerBrowseURLTool()
// (build-tagged): it requires a host-side headless browser (rod/Chromium)
// that is unavailable in WASM builds, so it is excluded from the WASM
// tool set rather than advertised as a tool that can never succeed.
//
// Memory operations (add/read/list/delete/search) are exposed as the
// consolidated manage_memory tool registered in
// pkg/agent/tool_registrations.go. The legacy add_memory / read_memory /
// list_memories / delete_memory handlers were removed once manage_memory
// covered the full surface.
//
// Subagent tools (run_subagent / run_parallel_subagents) are registered
// here using the function-pointer pattern established in Batch A2.
// Each exports a function pointer (RunSubagentFunc, RunParallelSubagentsFunc)
// that pkg/agent sets at startup, capturing the *Agent reference in a
// closure so the handlers don't need direct *Agent access. SP-059 Phase 3b
// removed earlier stub entries that returned hardcoded errors; these new
// ToolHandler implementations delegate to the canonical seed-registry
// dispatch path via the function pointers.
//
// To register all tools with a registry:
//
//	registry := tools.NewToolRegistry()
//	for _, h := range tools.AllTools() {
//	    registry.Register(h)
//	}
func AllTools() []ToolHandler {
	tools := []ToolHandler{
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
		// Consolidated memory management tool (replaces individual memory tools)
		&manageMemoryHandler{},
		// Settings management tool
		&manageSettingsHandler{},
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
		// Note: browse_url is registered via registerBrowseURLTool() (build-tagged)
		// because it requires a host-side headless browser unavailable in WASM.
		&webSearchHandler{},
		&semanticSearchHandler{},
		// Image/analysis tools
		&analyzeImageContentHandler{},
		&analyzeUIScreenshotHandler{},
		// SP-109 Phase 3 Batch A2 — change-tracking & automate tools
		&listAutomateWorkflowsHandler{},
		&listChangesHandler{},
		&revertMyChangesHandler{},
		&recoverFileHandler{},
		// SP-109 Phase 3 Batch A3 — agent-dependent function-pointer tools
		&createPullRequestHandler{},
		&runAutomateHandler{},
		&mcpRefreshHandler{},
		// SP-109 Phase 3 Batch B — subagent function-pointer tools
		&runSubagentHandler{},
		&runParallelSubagentsHandler{},
		// SP-109 Phase 3 Batch C — clarification function-pointer tools
		&requestClarificationHandler{},
		&respondClarificationHandler{},
		// Preview port registration (platform workspaces)
		&registerPreviewPortHandler{},
	}
	// Code intelligence graph tools — platform-specific (nil on WASM via
	// build-tagged stub).
	tools = append(tools, registerBrowseURLTool()...)
	return append(tools, registerCodegraphTools()...)
}
