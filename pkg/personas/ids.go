package personas

// Canonical persona IDs. Use these constants instead of string literals so
// that renames stay coupled to a single point of change.
//
// IDs here must match the `id` fields in pkg/personas/configs/*.json after
// normalization (lowercase, dashes → underscores). Aliases live in each
// persona's JSON `aliases` array and are resolved at lookup time, not here.
const (
	IDOrchestrator       = "orchestrator"
	IDGeneral            = "general"
	IDCoder              = "coder"
	IDRefactor           = "refactor"
	IDDebugger           = "debugger"
	IDTester             = "tester"
	IDReviewer           = "reviewer"
	IDResearcher         = "researcher"
	IDWebScraper         = "web_scraper"
	IDComputerUser       = "computer_user"
	IDCoordinator        = "coordinator"
	IDProjectPlanner     = "project_planner"
)
