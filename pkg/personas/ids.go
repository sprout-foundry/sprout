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
	IDTester       = "tester"
	IDReviewer     = "reviewer"
	IDResearcher   = "researcher"
	IDCoordinator  = "coordinator"
	IDComputerUser = "computer_user"
)

// Retired persona IDs. These personas were consolidated in 2026-09 when the
// minimum capable model made narrow specialists redundant:
//   - refactor → coder (behavior-preservation is a task constraint, not a
//     tool profile)
//   - debugger → coder (debugging is a workflow; tool set was a near-superset)
//   - web_scraper → researcher (researcher already carries the web toolset)
//
// The IDs survive as ALIASES on their merge targets (default_personas.json)
// so old configs, workflow files, and muscle memory keep resolving. Do not
// reuse them as canonical IDs.
const (
	IDRefactor   = "refactor"    // alias of IDCoder
	IDDebugger   = "debugger"    // alias of IDCoder
	IDWebScraper = "web_scraper" // alias of IDResearcher
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
