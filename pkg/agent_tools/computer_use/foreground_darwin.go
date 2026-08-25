//go:build darwin

package computer_use

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// foregroundTimeout caps how long osascript can run before we give up.
const foregroundTimeout = 3 * time.Second

func init() {
	getForegroundAppImpl = getForegroundAppDarwin
}

// getForegroundAppDarwin queries the frontmost macOS application via System
// Events. Returns both the human-readable name and the bundle identifier
// in a single osascript invocation (~150ms typical).
//
// Output format: "AppName\tBundleID\n" — both fields tab-separated on a
// single line. Whitespace is trimmed from each part.
func getForegroundAppDarwin() (ForegroundInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), foregroundTimeout)
	defer cancel()

	// Combine name + bundle identifier into a single tab-separated line.
	// Example output: "Safari\tcom.apple.Safari\n"
	script := `tell application "System Events" to set out to (name of (first application process whose frontmost is true)) & "\t" & (bundle identifier of (first application process whose frontmost is true))`

	out, err := foregroundRunner(ctx, "osascript", "-e", script)
	if err != nil {
		return ForegroundInfo{}, fmt.Errorf("osascript: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 {
		return ForegroundInfo{}, fmt.Errorf("osascript: unexpected output %q", string(out))
	}
	return ForegroundInfo{
		AppName:  strings.TrimSpace(parts[0]),
		BundleID: strings.TrimSpace(parts[1]),
	}, nil
}
