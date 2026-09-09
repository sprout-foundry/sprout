package computer_use

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// NewPlatformBackend returns the best available real backend for the current
// host, or a descriptive error when the platform/toolchain can't support
// computer use. Callers that get an error should keep the default MockBackend
// and surface the message to the user (e.g. "install cliclick").
func NewPlatformBackend() (ComputerBackend, error) {
	capTool, cliTool, err := detectTools(runtime.GOOS)
	if err != nil {
		return nil, err
	}
	return &subprocessBackend{
		os:      runtime.GOOS,
		tmpDir:  os.TempDir(),
		capTool: capTool,
		cliTool: cliTool,
	}, nil
}

// PlatformSupport describes whether the current host can run computer use and,
// if not, why. Used by the "Test connection" diagnostic and the persona
// activation check.
type PlatformSupport struct {
	Supported bool   `json:"supported"`
	OS        string `json:"os"`
	Reason    string `json:"reason,omitempty"` // populated when Supported is false
}

// CheckPlatformSupport reports whether a real backend can be constructed
// without actually taking control of the desktop.
func CheckPlatformSupport() PlatformSupport {
	if _, _, err := detectTools(runtime.GOOS); err != nil {
		return PlatformSupport{Supported: false, OS: runtime.GOOS, Reason: err.Error()}
	}
	return PlatformSupport{Supported: true, OS: runtime.GOOS}
}

// PermissionCheck reports the status of one OS permission computer use needs.
type PermissionCheck struct {
	Name    string `json:"name"`   // e.g. "Screen Recording", "Accessibility"
	OK      bool   `json:"ok"`     // true when granted
	Detail  string `json:"detail"` // how it was probed / what happened
	FixHint string `json:"fix_hint,omitempty"`
}

// CheckPermissions probes the two macOS TCC permissions computer use needs
// without spawning dialogs: it runs each backing tool in a harmless way and
// inspects the outcome. On non-darwin platforms it returns a single "n/a"
// entry (Linux input works without an Accessibility-style grant once the
// tools are installed).
func CheckPermissions() []PermissionCheck {
	if runtime.GOOS != "darwin" {
		return []PermissionCheck{{
			Name:   "permissions",
			OK:     true,
			Detail: "no TCC permissions required on " + runtime.GOOS,
		}}
	}

	var out []PermissionCheck

	// Screen Recording: screencapture either exits 1 ("could not create image
	// from display") or writes a 1x1 placeholder PNG when denied. Probe with a
	// capture to a throwaway path and check both signals.
	{
		tmp := filepath.Join(os.TempDir(), "sprout-cu-diag-capture.png")
		_ = os.Remove(tmp)
		probe, _ := NewPlatformBackend()
		check := PermissionCheck{
			Name:    "Screen Recording",
			FixHint: "System Settings → Privacy & Security → Screen Recording → enable the app that runs sprout",
		}
		if probe == nil {
			check.Detail = "backend unavailable"
			check.OK = false
		} else if sb, ok := probe.(*subprocessBackend); ok {
			_, dims, err := sb.Screenshot(nil)
			switch {
			case err != nil:
				check.Detail = "capture failed: " + err.Error()
			case dims.Width <= 1 || dims.Height <= 1:
				check.Detail = "capture returned a degenerate 1x1 image (permission denied)"
			default:
				check.Detail = fmt.Sprintf("capture OK (%dx%d)", dims.Width, dims.Height)
				check.OK = true
			}
		}
		out = append(out, check)
	}

	// Accessibility: cliclick prints "Accessibility privileges not enabled"
	// on stderr when denied. Probe with a modifier key-down + key-up pair —
	// no keystroke is emitted (a bare shift press does nothing on its own),
	// but the call exercises the same event-injection path as real input.
	{
		outBytes, err := commandRunner("cliclick", "kd:shift", "ku:shift")
		combined := strings.ToLower(strings.TrimSpace(string(outBytes)))
		if err != nil {
			combined = strings.ToLower(strings.TrimSpace(string(outBytes) + " " + err.Error()))
		}
		check := PermissionCheck{
			Name:    "Accessibility",
			FixHint: "System Settings → Privacy & Security → Accessibility → enable the app that runs sprout",
		}
		switch {
		case err != nil && strings.Contains(combined, "not found"):
			check.Detail = "cliclick not installed (brew install cliclick)"
		case strings.Contains(combined, "accessibility privileges not enabled"):
			check.Detail = "cliclick reports Accessibility privileges not enabled"
		case err != nil:
			check.Detail = "probe failed: " + strings.TrimSpace(err.Error())
		default:
			check.Detail = "cliclick key-tap accepted (no accessibility warning)"
			check.OK = true
		}
		out = append(out, check)
	}

	return out
}
