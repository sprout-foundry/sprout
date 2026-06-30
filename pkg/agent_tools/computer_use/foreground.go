package computer_use

import (
	"context"
	"errors"
	"os/exec"
)

// ErrForegroundUnavailable is returned by GetForegroundApp on platforms
// where foreground-window detection is not supported (Wayland, Windows,
// headless). Callers should log and skip the denylist gate gracefully.
var ErrForegroundUnavailable = errors.New("foreground-app detection not supported on this platform")

// ForegroundInfo describes the app currently in the foreground.
type ForegroundInfo struct {
	// AppName is the human-readable application name.
	AppName string

	// BundleID is the macOS bundle identifier (e.g., "com.apple.Safari").
	// Empty on Linux.
	BundleID string

	// WindowClass is the X11 WM_CLASS[Name] field (e.g., "Navigator").
	// Empty on macOS.
	WindowClass string

	// WindowTitle is the active window's title.
	WindowTitle string
}

// foregroundRunner is the package-level function used to execute the
// platform-specific foreground-detection commands. Tests override it to
// avoid invoking real osascript/xdotool. Production behavior uses
// exec.CommandContext with the provided context for timeout enforcement.
var foregroundRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// getForegroundAppImpl is the platform-specific implementation of
// GetForegroundApp. Each platform file (foreground_darwin.go,
// foreground_linux.go, foreground_other.go) initializes it in init()
// so that the build-tag-free GetForegroundApp() in this file delegates
// to the correct implementation. The default below returns
// ErrForegroundUnavailable — it is always overwritten by a platform init().
var getForegroundAppImpl func() (ForegroundInfo, error) = func() (ForegroundInfo, error) {
	return ForegroundInfo{}, ErrForegroundUnavailable
}

// GetForegroundApp returns the foreground-app tuple for the current
// platform. Returns ErrForegroundUnavailable when detection is not
// supported. Implementations live in platform-specific files:
//
//   - foreground_darwin.go (macOS, osascript-based)
//   - foreground_linux.go  (Linux/X11, xdotool + wmctrl)
//   - foreground_other.go  (stub for all other platforms)
func GetForegroundApp() (ForegroundInfo, error) {
	return getForegroundAppImpl()
}
