package agent

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// debugLogEnabled gates verbose informational logging from package-level
// (non-Agent-method) call sites such as turn_embedding.go and
// proactive_context.go. Toggleable at runtime; defaults to whatever the
// SPROUT_DEBUG environment variable indicates at package init.
//
// Real errors / warnings (failed embeds, unexpected nil providers, etc.)
// still log unconditionally via plain log.Printf — debugLogf is only for
// the routine "skipping" / "stored" / "retrieved N candidates" lines that
// previously spammed stderr on every turn.
var debugLogEnabled atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEBUG"))) {
	case "1", "true", "yes", "on":
		debugLogEnabled.Store(true)
	}
}

// SetPackageDebugLogging toggles the debug gate at runtime. Useful for tests
// and for the agent's --debug flag wiring.
func SetPackageDebugLogging(enabled bool) {
	debugLogEnabled.Store(enabled)
}

// debugLogf is a leveled wrapper around log.Printf that fires only when
// debug logging is enabled. Use this for routine operational logs that
// should not spam stderr in production.
func debugLogf(format string, args ...any) {
	if debugLogEnabled.Load() {
		log.Printf(format, args...)
	}
}
