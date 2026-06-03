package console

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Persona ANSI foreground colors, picked to be distinct on a default
// dark-background terminal. The mapping is deterministic so the same
// persona shows the same color across sessions and across CLI lines.
//
// Intentional divergence from the WebUI's persona palette at
// `packages/ui/src/utils/personaColors.ts`: the CLI uses standard
// 8-color ANSI codes (whose actual rendering is set by the user's
// terminal theme) while the WebUI uses GitHub-themed hex values. As a
// result the *exact* color may differ, but the semantic class
// (warm/cool/neutral, distinctness from neighbors) holds across both
// surfaces. Two persona IDs were tightened to match the WebUI's
// reassignments:
//   - orchestrator: stays bold (the primary delegation target) but
//     defers to the terminal's "yellow" mapping so it doesn't collide
//     with text on a black-on-white terminal config.
//   - executive_assistant: bold cyan to keep it visually adjacent to
//     "orchestrator" without overlapping coder.
//
// Update both files together when reassigning a persona.
const (
	personaColorCoder        = "\033[36m"      // cyan
	personaColorTester       = "\033[32m"      // green
	personaColorDebugger     = "\033[33m"      // yellow
	personaColorResearcher   = "\033[35m"      // magenta
	personaColorReviewer     = "\033[34m"      // blue
	personaColorRefactor     = "\033[37m"      // white
	personaColorOrchestrator = "\033[1;33m"    // bold yellow — matches WebUI amber, readable on light terminals
	personaColorEA           = "\033[1;36m"    // bold cyan — top-level coordinator
	personaColorGeneral      = "\033[90m"      // dim gray fallback
	personaResetANSI         = "\033[0m"
)

// PersonaColor returns the ANSI color escape for the given persona ID, or
// the empty string when colors are disabled (NO_COLOR / non-TTY default).
//
// Unknown personas get a dim-gray fallback so they're still visually
// grouped without colliding with any of the well-known IDs.
func PersonaColor(personaID string) string {
	if !envutil.ResolveColorPreference(true) {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(personaID)) {
	case personas.IDCoder:
		return personaColorCoder
	case personas.IDTester:
		return personaColorTester
	case personas.IDDebugger:
		return personaColorDebugger
	case personas.IDResearcher:
		return personaColorResearcher
	case personas.IDReviewer, "code_reviewer":
		return personaColorReviewer
	case personas.IDRefactor:
		return personaColorRefactor
	case personas.IDOrchestrator:
		return personaColorOrchestrator
	case personas.IDCoordinator, "executive_assistant":
		return personaColorEA
	case "":
		return ""
	default:
		return personaColorGeneral
	}
}

// PersonaBadge renders a colored "[persona]" prefix for a tool-timeline line.
// Returns the empty string for depth 0 or empty persona — the primary agent's
// own tool lines get no badge so the existing UX is preserved.
//
// Example: PersonaBadge(1, "coder") → "\033[36m[coder]\033[0m "
//          PersonaBadge(0, "orchestrator") → ""
func PersonaBadge(depth int, personaID string) string {
	personaID = strings.TrimSpace(personaID)
	if depth <= 0 || personaID == "" {
		return ""
	}
	color := PersonaColor(personaID)
	if color == "" {
		return fmt.Sprintf("[%s] ", personaID)
	}
	return fmt.Sprintf("%s[%s]%s ", color, personaID, personaResetANSI)
}

// PersonaIndent returns the leading whitespace for a depth-N tool line.
// Two spaces per depth level keeps the timeline scannable without taking
// over the line. Depth 0 returns "" so primary-agent output is unchanged.
func PersonaIndent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}
