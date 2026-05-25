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

// packageLogger is an optional structured logger used by package-level
// functions that don't have direct access to an *Agent instance.
// Set via SetPackageLogger during agent initialization.
var packageLogger atomic.Pointer[AgentLogger]

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

// SetPackageLogger sets the package-level AgentLogger that package-level
// functions (without an *Agent receiver) use for structured logging.
// Called during agent initialization so embedding, proactive context, etc.
// all route through the same logger with session context.
func SetPackageLogger(l *AgentLogger) {
	if l != nil {
		packageLogger.Store(l)
	}
}

// debugLogf is a leveled wrapper around log.Printf that fires only when
// debug logging is enabled. Use this for routine operational logs that
// should not spam stderr in production.
//
// When a packageLogger is set (via SetPackageLogger), debug messages are
// routed through it for structured output with session context.
func debugLogf(format string, args ...any) {
	if !debugLogEnabled.Load() {
		return
	}
	if pl := packageLogger.Load(); pl != nil {
		pl.Debug(format, args...)
		return
	}
	log.Printf(format, args...)
}

// packageLogf logs an info-level message through the package logger when
// available, otherwise falls back to log.Printf. Use this for informational
// messages from package-level functions.
func packageLogf(format string, args ...any) {
	if pl := packageLogger.Load(); pl != nil {
		pl.Info(format, args...)
		return
	}
	log.Printf(format, args...)
}

// packageLogWarnf logs a warning-level message through the package logger when
// available, otherwise falls back to log.Printf. Use this for warning messages
// from package-level functions.
func packageLogWarnf(format string, args ...any) {
	if pl := packageLogger.Load(); pl != nil {
		pl.Warn(format, args...)
		return
	}
	log.Printf(format, args...)
}

// packageLogErrorf logs an error-level message through the package logger when
// available, otherwise falls back to log.Printf. Use this for error messages
// from package-level functions.
func packageLogErrorf(format string, args ...any) {
	if pl := packageLogger.Load(); pl != nil {
		pl.Error(format, args...)
		return
	}
	log.Printf(format, args...)
}
