package tools

// AllTools returns all available tool handlers for registration.
// This is the central registration point for the interface-based tool system.
//
// browse_url, vision tools and run_automate are registered conditionally via
// build-tagged registrars (reduced or nil on WASM). The design tools are
// shared: in the browser build design_render rasterizes through the host
// page (pkg/webcontent/browser_page_js.go).
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
		&listAutomateWorkflowsHandler{},
		&listChangesHandler{},
		&revertMyChangesHandler{},
		&recoverFileHandler{},
		&createPullRequestHandler{},
		&mcpRefreshHandler{},
		&runSubagentHandler{},
		&runParallelSubagentsHandler{},
		&reviewChangesHandler{},
		&checkSubagentHandler{},
		&stopSubagentHandler{},
		&requestClarificationHandler{},
		&respondClarificationHandler{},
		&registerPreviewPortHandler{},
		&designValidateHandler{},
		&designAssetsHandler{},
		&designBriefHandler{},
		&designExportHandler{},
		&designSyncHandler{},
		&designRenderHandler{},
		&designCritiqueHandler{},
		&designImportSketchHandler{},
	}
	// Platform-specific tools (nil on WASM via build-tagged stubs).
	tools = append(tools, registerBrowseURLTool()...)
	tools = append(tools, registerVisionTools()...)
	tools = append(tools, registerRunAutomateTool()...)
	tools = append(tools, registerCodegraphTools()...)
	return append(tools, registerSearchTool()...)
}
