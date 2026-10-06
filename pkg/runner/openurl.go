package runner

import (
	"net/url"
	"os/exec"
	"runtime"
)

// OpenURL opens an http(s) URL in the user's browser, best effort: the link
// flow prints the URL too, so a failure here costs nothing. The URL comes
// from the platform's response, so anything else (file:, app launchers, a
// value that would parse as a flag) is not opened.
func OpenURL(raw string) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return
	}
	target := u.String()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target) //nolint:gosec // G204: validated http(s) URL
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target) //nolint:gosec // G204: validated http(s) URL
	default:
		cmd = exec.Command("xdg-open", target) //nolint:gosec // G204: validated http(s) URL
	}
	_ = cmd.Start()
}
