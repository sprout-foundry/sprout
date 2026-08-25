//go:build linux

package computer_use

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// foregroundTimeout caps how long xdotool/wmctrl can run before we give up.
const foregroundTimeout = 3 * time.Second

// runForegroundCmd is the linux-specific function used to execute
// foreground-detection commands with a context. Overridable in tests
// to avoid invoking real xdotool/wmctrl.
//
// Note: this duplicates foregroundRunner (defined in foreground.go).
// It exists as a separate package var so linux tests can independently
// mock subprocess calls (xdotool + wmctrl, three distinct invocations)
// without affecting the shared foregroundRunner that other callers may
// be using. Darwin has a single subprocess call, so it goes through
// foregroundRunner directly.
var runForegroundCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// lookPath is the linux-specific function used to check for tool presence.
// Overridable in tests to control whether wmctrl is "installed".
//
// Not safe for t.Parallel(): tests that swap lookPath via restoreLookPath
// must not run in parallel with each other, since the global var is shared.
var lookPath = exec.LookPath

func init() {
	getForegroundAppImpl = getForegroundAppLinux
}

// getForegroundAppLinux queries the active X11 window via xdotool and
// (optionally) enriches the result with WM_CLASS via wmctrl. When wmctrl
// is not installed, WindowClass is left empty — denylist matching falls
// back to WindowTitle only.
func getForegroundAppLinux() (ForegroundInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), foregroundTimeout)
	defer cancel()

	// Step 1: get active window ID (hex).
	widOut, err := runForegroundCmd(ctx, "xdotool", "getactivewindow")
	if err != nil {
		return ForegroundInfo{}, fmt.Errorf("xdotool getactivewindow: %w", err)
	}
	wid := strings.TrimSpace(string(widOut))
	if wid == "" {
		return ForegroundInfo{}, fmt.Errorf("xdotool: no active window")
	}

	// Step 2: get the active window title.
	titleOut, err := runForegroundCmd(ctx, "xdotool", "getactivewindow", "getwindowname")
	if err != nil {
		return ForegroundInfo{}, fmt.Errorf("xdotool getwindowname: %w", err)
	}
	title := strings.TrimSpace(string(titleOut))

	// Step 3: try wmctrl for window class (optional — degrades gracefully).
	var windowClass string
	if _, err := lookPath("wmctrl"); err == nil {
		wmctrlOut, werr := runForegroundCmd(ctx, "wmctrl", "-lx")
		if werr == nil {
			// wmctrl -lx output: "<wid> <desktop> <WM_CLASS[Name]> <host> <title>"
			// Find the line whose first field matches our wid.
			for _, line := range strings.Split(string(wmctrlOut), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 3 && fields[0] == wid {
					windowClass = fields[2]
					break
				}
			}
		}
	}

	return ForegroundInfo{
		WindowTitle: title,
		WindowClass: windowClass,
		// AppName: not directly available on Linux — denylist matching
		// uses WindowClass + WindowTitle only.
	}, nil
}
