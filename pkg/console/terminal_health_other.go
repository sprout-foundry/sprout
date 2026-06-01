//go:build !unix || js

package console

// GroundTruthTermios is a no-op on non-Unix platforms (Windows, js/wasm)
// where termios ioctls don't exist. The terminal health check / recovery
// features degrade gracefully — all methods are safe to call.

// GroundTruthTermios holds nothing on non-Unix platforms.
type GroundTruthTermios struct{}

// CaptureGroundTruth returns nil on non-Unix (nothing to snapshot).
func CaptureGroundTruth() *GroundTruthTermios { return nil }

// Restore is a no-op on non-Unix.
func (g *GroundTruthTermios) Restore() error { return nil }

// IsTerminalSane always returns true on non-Unix.
func (g *GroundTruthTermios) IsTerminalSane() bool { return true }

// EnsureSane is a no-op on non-Unix, always returns false.
func (g *GroundTruthTermios) EnsureSane() bool { return false }

// Fd returns -1 on non-Unix.
func (g *GroundTruthTermios) Fd() int { return -1 }
