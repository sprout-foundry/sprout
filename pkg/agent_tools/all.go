package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the interface-based tool system.
//
// browse_url, vision tools, run_automate, design_render,
// design_import_sketch, and design_critique are registered conditionally via
// build-tagged stubs (nil on WASM).
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
		&manageMemoryHandler{},
		&manageSettingsHandler{},
		&todoWriteHandler{},
		&todoReadHandler{},
		&askUserHandler{},
		&patchStructuredFileHandler{},
		&commitHandler{},
		&gitHandler{},
		&activateSkillHandler{},
		// browse_url is registered via registerBrowseURLTool() (build-tagged)
		&webSearchHandler{},
		&semanticSearchHandler{},
		&listAutomateWorkflowsHandler{},
		&listChangesHandler{},
		&revertMyChangesHandler{},
		&recoverFileHandler{},
		&createPullRequestHandler{},
		&mcpRefreshHandler{},
		&runSubagentHandler{},
		&runParallelSubagentsHandler{},
		&requestClarificationHandler{},
		&respondClarificationHandler{},
		&registerPreviewPortHandler{},
		&designValidateHandler{},
		&designAssetsHandler{},
	}
	// Platform-specific tools (nil on WASM via build-tagged stubs).
	tools = append(tools, registerBrowseURLTool()...)
	tools = append(tools, registerVisionTools()...)
	tools = append(tools, registerRunAutomateTool()...)
	tools = append(tools, registerCodegraphTools()...)
	tools = append(tools, registerDesignRenderTools()...)
	tools = append(tools, registerDesignImportSketchTools()...)
	tools = append(tools, registerDesignCritiqueTools()...)
	tools = append(tools, registerDesignExportTools()...)
	tools = append(tools, registerDesignSyncTools()...)
	tools = append(tools, registerDesignBriefTools()...)
	return append(tools, registerSearchTool()...)
}
