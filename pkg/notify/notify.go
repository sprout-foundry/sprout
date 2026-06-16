// Package notify provides cross-platform OS-level desktop notifications.
//
// It detects the current operating system and uses the appropriate notification
// tool (osascript on macOS, notify-send on Linux, PowerShell on Windows).
// When no notification tool is available, it returns a no-op notifier.
package notify

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

// Notifier sends OS-level notifications.
type Notifier interface {
	Notify(title, message string) error
}

// New returns the platform-appropriate Notifier, or a NoopNotifier if no
// notification tool is available on the current platform.
func New() Notifier {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath(osascriptCmd); err == nil {
			return &darwinNotifier{}
		}
		log.Printf("notify: osascript not found, using noop notifier")
		return NoopNotifier{}
	case "linux":
		if _, err := exec.LookPath(notifySendCmd); err == nil {
			return &linuxNotifier{}
		}
		log.Printf("notify: notify-send not found, using noop notifier")
		return NoopNotifier{}
	case "windows":
		if _, err := exec.LookPath(powershellCmd); err == nil {
			return &windowsNotifier{}
		}
		log.Printf("notify: powershell not found, using noop notifier")
		return NoopNotifier{}
	default:
		log.Printf("notify: unsupported OS %q, using noop notifier", runtime.GOOS)
		return NoopNotifier{}
	}
}

// NoopNotifier does nothing when Notify is called.
type NoopNotifier struct{}

func (NoopNotifier) Notify(title, message string) error {
	return nil
}

// --- macOS ---

// osascriptCmd can be overridden in tests.
var osascriptCmd = "osascript"

type darwinNotifier struct{}

func (d *darwinNotifier) Notify(title, message string) error {
	// Escape single quotes and backslashes for AppleScript string literal
	t := strings.ReplaceAll(title, "\\", "\\\\")
	t = strings.ReplaceAll(t, "'", "\\'")
	m := strings.ReplaceAll(message, "\\", "\\\\")
	m = strings.ReplaceAll(m, "'", "\\'")
	script := fmt.Sprintf("display notification \"%s\" with title \"%s\"", m, t)
	_, err := runCommand(exec.Command(osascriptCmd, "-e", script))
	return err
}

// --- Linux ---

// notifySendCmd can be overridden in tests.
var notifySendCmd = "notify-send"

type linuxNotifier struct{}

func (l *linuxNotifier) Notify(title, message string) error {
	_, err := runCommand(exec.Command(notifySendCmd, title, message))
	return err
}

// --- Windows ---

// powershellCmd can be overridden in tests.
var powershellCmd = "powershell"

type windowsNotifier struct{}

func (w *windowsNotifier) Notify(title, message string) error {
	// Use PowerShell's built-in toast notification via BurntToast module if available,
	// otherwise fall back to a simple message box
	script := fmt.Sprintf(
		"[System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms'); "+
			"$balloon = New-Object System.Windows.Forms.NotifyIcon; "+
			"$balloon.Icon = [System.Drawing.SystemIcons]::ApplicationInfo; "+
			"$balloon.BalloonTipText = %q; "+
			"$balloon.BalloonTipTitle = %q; "+
			"$balloon.Visible = $true; "+
			"$balloon.ShowBalloonTip(5000); "+
			"$balloon.Dispose()",
		message, title,
	)
	_, err := runCommand(exec.Command(powershellCmd, "-Command", script))
	return err
}

// runCommand executes a command and returns its combined output.
// It can be overridden in tests.
var runCommand = func(cmd *exec.Cmd) ([]byte, error) {
	return cmd.CombinedOutput()
}
