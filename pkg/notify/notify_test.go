package notify

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NoopNotifier
// ---------------------------------------------------------------------------

func TestNoopNotifier_DoesNothing(t *testing.T) {
	err := NoopNotifier{}.Notify("title", "message")
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// New() — backend selection
// ---------------------------------------------------------------------------

func TestNew_ToolNotFound_ReturnsNoop(t *testing.T) {
	// Only test the platform we're actually running on. Set the cmd var to a
	// path that can't possibly exist so exec.LookPath fails.
	orig := ""
	switch runtime.GOOS {
	case "darwin":
		orig = osascriptCmd
		osascriptCmd = "/definitely/not/a/real/osascript"
		defer func() { osascriptCmd = orig }()
	case "linux":
		orig = notifySendCmd
		notifySendCmd = "/definitely/not/a/real/notify-send"
		defer func() { notifySendCmd = orig }()
	case "windows":
		orig = powershellCmd
		powershellCmd = "/definitely/not/a/real/powershell"
		defer func() { powershellCmd = orig }()
	default:
		// Unsupported OS already returns NoopNotifier — test that path.
		n := New()
		assert.IsType(t, NoopNotifier{}, n)
		return
	}

	n := New()
	assert.IsType(t, NoopNotifier{}, n)
}

func TestNew_UnsupportedOS_ReturnsNoop(t *testing.T) {
	// We can't change runtime.GOOS so we can only directly test this when
	// actually running on an unsupported OS (e.g. freebsd).
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("running on a supported OS — skipping unsupported-OS path")
	}
	n := New()
	assert.IsType(t, NoopNotifier{}, n)
}

func TestNew_ToolAvailable_ReturnsPlatformNotifier(t *testing.T) {
	// Override the cmd var to a binary we know exists ("sh") so LookPath
	// succeeds, then check the returned type.
	orig := ""
	var expectedType Notifier
	switch runtime.GOOS {
	case "darwin":
		orig = osascriptCmd
		osascriptCmd = "sh"
		defer func() { osascriptCmd = orig }()
		expectedType = &darwinNotifier{}
	case "linux":
		orig = notifySendCmd
		notifySendCmd = "sh"
		defer func() { notifySendCmd = orig }()
		expectedType = &linuxNotifier{}
	case "windows":
		orig = powershellCmd
		powershellCmd = "sh"
		defer func() { powershellCmd = orig }()
		expectedType = &windowsNotifier{}
	default:
		t.Skip("unsupported OS — cannot test platform notifier path")
	}

	n := New()
	assert.IsType(t, expectedType, n)
}

// ---------------------------------------------------------------------------
// darwinNotifier — command construction
// ---------------------------------------------------------------------------

func TestDarwinNotifier_ConstructsCorrectCommand(t *testing.T) {
	var captured *exec.Cmd
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() { runCommand = orig }()

	err := (&darwinNotifier{}).Notify("My Title", "Hello World")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "osascript", captured.Path)
	assert.Equal(t, []string{
		"osascript", "-e",
		`display notification "Hello World" with title "My Title"`,
	}, captured.Args)
}

func TestDarwinNotifier_EscapesQuotesAndBackslashes(t *testing.T) {
	var captured *exec.Cmd
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() { runCommand = orig }()

	titleIn := "It's a \\test"
	msgIn := "Back\\slash & 'quotes'"

	err := (&darwinNotifier{}).Notify(titleIn, msgIn)
	assert.NoError(t, err)
	require.NotNil(t, captured)
	require.Len(t, captured.Args, 3)

	// Compute the expected script the same way the implementation does:
	// 1) replace \ with \\  2) replace ' with \'
	escTitle := strings.ReplaceAll(titleIn, "\\", "\\\\")
	escTitle = strings.ReplaceAll(escTitle, "'", "\\'")
	escMsg := strings.ReplaceAll(msgIn, "\\", "\\\\")
	escMsg = strings.ReplaceAll(escMsg, "'", "\\'")

	expected := fmt.Sprintf("display notification \"%s\" with title \"%s\"", escMsg, escTitle)
	assert.Equal(t, expected, captured.Args[2])
}

func TestDarwinNotifier_AllowsCustomCommand(t *testing.T) {
	origCmd := osascriptCmd
	osascriptCmd = "/custom/path/to/osascript"
	origRun := runCommand
	var captured *exec.Cmd
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() {
		osascriptCmd = origCmd
		runCommand = origRun
	}()

	err := (&darwinNotifier{}).Notify("T", "M")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "/custom/path/to/osascript", captured.Path)
}

func TestDarwinNotifier_PropagatesCommandError(t *testing.T) {
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	defer func() { runCommand = orig }()

	err := (&darwinNotifier{}).Notify("T", "M")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// linuxNotifier — command construction
// ---------------------------------------------------------------------------

func TestLinuxNotifier_ConstructsCorrectCommand(t *testing.T) {
	var captured *exec.Cmd
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() { runCommand = orig }()

	err := (&linuxNotifier{}).Notify("My Title", "Hello World")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	// Args[0] is the original command name (exec.Command keeps the unqualified name)
	assert.Equal(t, []string{
		"notify-send", "My Title", "Hello World",
	}, captured.Args)
}

func TestLinuxNotifier_AllowsCustomCommand(t *testing.T) {
	origCmd := notifySendCmd
	notifySendCmd = "/usr/local/bin/notify-send"
	origRun := runCommand
	var captured *exec.Cmd
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() {
		notifySendCmd = origCmd
		runCommand = origRun
	}()

	err := (&linuxNotifier{}).Notify("T", "M")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "/usr/local/bin/notify-send", captured.Path)
}

func TestLinuxNotifier_PropagatesCommandError(t *testing.T) {
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	defer func() { runCommand = orig }()

	err := (&linuxNotifier{}).Notify("T", "M")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// windowsNotifier — command construction
// ---------------------------------------------------------------------------

func TestWindowsNotifier_ConstructsCorrectCommand(t *testing.T) {
	var captured *exec.Cmd
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() { runCommand = orig }()

	err := (&windowsNotifier{}).Notify("My Title", "Hello World")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "powershell", captured.Path)
	require.Len(t, captured.Args, 3)
	assert.Equal(t, "powershell", captured.Args[0])
	assert.Equal(t, "-Command", captured.Args[1])
	script := captured.Args[2]
	assert.Contains(t, script, "Hello World")
	assert.Contains(t, script, "My Title")
	assert.Contains(t, script, "NotifyIcon")
	assert.Contains(t, script, "ShowBalloonTip(5000)")
}

func TestWindowsNotifier_EscapesMessageProperly(t *testing.T) {
	var captured *exec.Cmd
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() { runCommand = orig }()

	err := (&windowsNotifier{}).Notify("Line\tTab", "Has \"quotes\" inside")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	script := captured.Args[2]
	// %q in Sprintf produces a Go-escaped, double-quoted string.
	assert.Contains(t, script, `"Has \"quotes\" inside"`)
	assert.Contains(t, script, `"Line\tTab"`)
}

func TestWindowsNotifier_AllowsCustomCommand(t *testing.T) {
	origCmd := powershellCmd
	powershellCmd = "/usr/bin/pwsh"
	origRun := runCommand
	var captured *exec.Cmd
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		captured = cmd
		return nil, nil
	}
	defer func() {
		powershellCmd = origCmd
		runCommand = origRun
	}()

	err := (&windowsNotifier{}).Notify("T", "M")
	assert.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "/usr/bin/pwsh", captured.Path)
}

func TestWindowsNotifier_PropagatesCommandError(t *testing.T) {
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	defer func() { runCommand = orig }()

	err := (&windowsNotifier{}).Notify("T", "M")
	assert.Error(t, err)
}
