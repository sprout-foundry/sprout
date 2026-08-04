package utils

import (
	"log/slog"
	"runtime/debug"
)

// SafeGo launches a goroutine with panic recovery and structured logging.
// It replaces the repeated defer-recover-log pattern used across fire-and-forget
// goroutines. The recovered panic and stack trace are logged at Error level
// with the provided name and optional attrs so the source is identifiable.
//
// Use SafeGo for goroutines whose only recovery need is logging — not for
// goroutines that need custom cleanup (closing channels, decrementing counters,
// sending error events) on panic. Those should keep an inline defer-recover.
func SafeGo(logger *slog.Logger, name string, fn func(), attrs ...slog.Attr) {
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				args := make([]any, 0, 4+len(attrs)*2)
				args = append(args, slog.String("name", name), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
				for _, a := range attrs {
					args = append(args, a)
				}
				logger.Error("goroutine panicked", args...)
			}
		}()
		fn()
	}()
}
