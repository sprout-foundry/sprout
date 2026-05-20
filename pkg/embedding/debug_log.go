package embedding

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// debugLogEnabled gates verbose progress / per-file informational logging
// from this package. Defaults to whatever SPROUT_DEBUG indicates at init.
//
// Real errors and warnings (failed embeds, corrupt store, unexpected nil
// providers, etc.) still log unconditionally via plain log.Printf — this
// gate only silences the routine "walk progress", "extraction progress",
// "embedding progress", and "skipping <file>" lines that previously
// flooded stderr on every refresh.
var debugLogEnabled atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEBUG"))) {
	case "1", "true", "yes", "on":
		debugLogEnabled.Store(true)
	}
}

// SetPackageDebugLogging toggles the debug gate at runtime. Useful for
// tests and for the agent's --debug flag wiring.
func SetPackageDebugLogging(enabled bool) {
	debugLogEnabled.Store(enabled)
}

// debugLogf wraps log.Printf for routine operational logs that should not
// spam stderr in production. No-op unless debug logging is enabled.
func debugLogf(format string, args ...any) {
	if debugLogEnabled.Load() {
		log.Printf(format, args...)
	}
}
