package credentials

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// debugLogEnabled gates routine, non-actionable informational logging from
// this package (storage-backend auto-detection, keyring availability, which
// backend was chosen). These lines previously printed on every single CLI
// invocation — e.g. "[credentials] Auto-detecting storage backend...",
// "[credentials] OS keyring not available, using file backend" — polluting
// user-facing output. They're diagnostics, not user messages, so they default
// to silent and only appear under SPROUT_DEBUG.
//
// Genuine, actionable failures (e.g. a credential that can't be read when one
// is required) still surface through normal error returns to the caller.
var debugLogEnabled atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEBUG"))) {
	case "1", "true", "yes", "on":
		debugLogEnabled.Store(true)
	}
}

// SetPackageDebugLogging toggles the debug gate at runtime (for the agent's
// --debug flag wiring and tests).
func SetPackageDebugLogging(enabled bool) {
	debugLogEnabled.Store(enabled)
}

// debugLogf wraps log.Printf for routine operational logs that should not spam
// stderr in production. No-op unless debug logging is enabled.
func debugLogf(format string, args ...any) {
	if debugLogEnabled.Load() {
		log.Printf(format, args...)
	}
}
