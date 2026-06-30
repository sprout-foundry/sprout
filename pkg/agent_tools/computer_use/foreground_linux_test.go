//go:build linux

package computer_use

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func restoreRunForegroundCmd(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	orig := runForegroundCmd
	runForegroundCmd = fn
	t.Cleanup(func() { runForegroundCmd = orig })
}

func restoreLookPath(t *testing.T, fn func(name string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

func TestForegroundLinux_MockSuccess_WithWmctrl(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	// wmctrl is "present" for this test.
	restoreLookPath(t, func(name string) (string, error) {
		if name == "wmctrl" {
			return "/usr/bin/wmctrl", nil
		}
		return "", errors.New("not found")
	})

	calls := 0
	restoreRunForegroundCmd(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		switch {
		case name == "xdotool" && len(args) == 1 && args[0] == "getactivewindow":
			return []byte("0x04000001\n"), nil
		case name == "xdotool" && len(args) == 2 && args[1] == "getwindowname":
			return []byte("My Document - gedit\n"), nil
		case name == "wmctrl":
			// wmctrl -lx output: "<wid> <desktop> <WM_CLASS[Name]> <host> <title>"
			return []byte("0x00000000 0  Navigator  host  Mozilla Firefox\n0x04000001 0  gedit  host  My Document\n"), nil
		}
		return nil, errors.New("unexpected call")
	})

	got, err := GetForegroundApp()
	if err != nil {
		t.Fatalf("GetForegroundApp() = %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 subprocess calls, got %d", calls)
	}
	if got.WindowTitle != "My Document - gedit" {
		t.Errorf("WindowTitle = %q, want %q", got.WindowTitle, "My Document - gedit")
	}
	if got.WindowClass != "gedit" {
		t.Errorf("WindowClass = %q, want %q (from wmctrl)", got.WindowClass, "gedit")
	}
	if got.AppName != "" {
		t.Errorf("AppName = %q, want empty (linux doesn't expose this)", got.AppName)
	}
	if got.BundleID != "" {
		t.Errorf("BundleID = %q, want empty (linux-only field)", got.BundleID)
	}
}

func TestForegroundLinux_MockSuccess_NoWmctrl(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	// wmctrl is "absent" for this test.
	restoreLookPath(t, func(name string) (string, error) {
		return "", errors.New("not found")
	})

	calls := 0
	restoreRunForegroundCmd(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		switch {
		case name == "xdotool" && len(args) == 1:
			return []byte("0x04000001\n"), nil
		case name == "xdotool" && len(args) == 2:
			return []byte("My Document\n"), nil
		}
		return nil, errors.New("unexpected call: " + name)
	})

	got, err := GetForegroundApp()
	if err != nil {
		t.Fatalf("GetForegroundApp() = %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 subprocess calls (no wmctrl), got %d", calls)
	}
	if got.WindowTitle != "My Document" {
		t.Errorf("WindowTitle = %q, want %q", got.WindowTitle, "My Document")
	}
	if got.WindowClass != "" {
		t.Errorf("WindowClass = %q, want empty (wmctrl absent)", got.WindowClass)
	}
}

func TestForegroundLinux_MockFailure_XdotoolMissing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	restoreRunForegroundCmd(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("exec: \"xdotool\": executable file not found in $PATH")
	})

	_, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want error when xdotool is missing")
	}
	if errors.Is(err, ErrForegroundUnavailable) {
		t.Errorf("err = ErrForegroundUnavailable, want a real xdotool-missing error")
	}
	if !strings.Contains(err.Error(), "xdotool") {
		t.Errorf("err = %q, want it to mention 'xdotool'", err)
	}
}

func TestForegroundLinux_MockFailure_NoActiveWindow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	restoreRunForegroundCmd(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Empty wid — common when no window is focused.
		return []byte(""), nil
	})

	_, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want error for empty wid")
	}
	if !strings.Contains(err.Error(), "no active window") {
		t.Errorf("err = %q, want it to mention 'no active window'", err)
	}
}
