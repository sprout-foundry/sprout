package personas

// Canonical persona IDs. Use these constants instead of string literals so
// that renames stay coupled to a single point of change.
//
// IDs here must match the `id` fields in pkg/personas/configs/*.json after
// normalization (lowercase, dashes → underscores). Aliases live in each
// persona's JSON `aliases` array and are resolved at lookup time, not here.
const (
	IDOrchestrator = "orchestrator"
	IDGeneral      = "general"
	IDCoder        = "coder"
	IDRefactor     = "refactor"
	IDDebugger     = "debugger"
	IDTester       = "tester"
	IDReviewer     = "reviewer"
	IDResearcher   = "researcher"
	IDWebScraper   = "web_scraper"
	IDCoordinator  = "coordinator"
	IDComputerUser = "computer_user"
)

// Canonical persona capability names. A capability is an explicit grant of
// agency that some personas have and others don't — e.g. the right to perform
// git writes, or (after SP-NNN spawn_policy) the right to spawn subagents.
// Use these constants rather than string literals so renames stay typesafe.
const (
	// CapabilityGitWrite — persona may perform git write operations (commit,
	// stage, push) via the dedicated commit tool and shell_command. Any persona
	// declaring this capability is allowed git-write operations.
	CapabilityGitWrite = "git_write"

	// CapabilityComputerUse — persona may drive the desktop (mouse, keyboard,
	// screenshots) via the computer_use tools. Held only by the computer_user
	// persona and gated further by the ComputerUse.Enabled config flag.
	CapabilityComputerUse = "computer_use"
)
