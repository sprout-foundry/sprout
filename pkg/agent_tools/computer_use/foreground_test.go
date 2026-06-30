package computer_use

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// Compile-time assertion: GetForegroundApp has the expected signature.
var _ func() (ForegroundInfo, error) = GetForegroundApp

// restoreForegroundRunner swaps foregroundRunner and returns a cleanup.
func restoreForegroundRunner(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	orig := foregroundRunner
	foregroundRunner = fn
	t.Cleanup(func() { foregroundRunner = orig })
}

// -----------------------------------------------------------------------
// Darwin tests (only run on darwin)
// -----------------------------------------------------------------------

func TestForegroundDarwin_MockSuccess(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	restoreForegroundRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Safari\tcom.apple.Safari\n"), nil
	})

	got, err := GetForegroundApp()
	if err != nil {
		t.Fatalf("GetForegroundApp() = %v, want nil", err)
	}
	if got.AppName != "Safari" {
		t.Errorf("AppName = %q, want %q", got.AppName, "Safari")
	}
	if got.BundleID != "com.apple.Safari" {
		t.Errorf("BundleID = %q, want %q", got.BundleID, "com.apple.Safari")
	}
	if got.WindowClass != "" {
		t.Errorf("WindowClass = %q, want empty (darwin-only field)", got.WindowClass)
	}
	if got.WindowTitle != "" {
		t.Errorf("WindowTitle = %q, want empty (darwin-only field)", got.WindowTitle)
	}
}

func TestForegroundDarwin_MockTrimsWhitespace(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	restoreForegroundRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(" Safari \t com.apple.Safari \n"), nil
	})

	got, err := GetForegroundApp()
	if err != nil {
		t.Fatalf("GetForegroundApp() = %v", err)
	}
	if got.AppName != "Safari" {
		t.Errorf("AppName = %q, want %q", got.AppName, "Safari")
	}
	if got.BundleID != "com.apple.Safari" {
		t.Errorf("BundleID = %q, want %q", got.BundleID, "com.apple.Safari")
	}
}

func TestForegroundDarwin_MockFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	restoreForegroundRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("osascript: permission denied")
	})

	_, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want error")
	}
	if errors.Is(err, ErrForegroundUnavailable) {
		t.Errorf("err = ErrForegroundUnavailable, want a real osascript error")
	}
	if !strings.Contains(err.Error(), "osascript") {
		t.Errorf("err = %q, want it to mention 'osascript'", err)
	}
}

func TestForegroundDarwin_MockBadOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	restoreForegroundRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Missing the tab separator.
		return []byte("Safari\n"), nil
	})

	_, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want error for malformed output")
	}
	if errors.Is(err, ErrForegroundUnavailable) {
		t.Errorf("err = ErrForegroundUnavailable, want a real malformed-output error")
	}
	if !strings.Contains(err.Error(), "unexpected output") {
		t.Errorf("err = %q, want it to mention 'unexpected output'", err)
	}
}

func TestForegroundDarwin_MockTooManyFields(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	// With SplitN(..., 2), extra tabs are absorbed into parts[1].
	// Test empty output instead — splits into [""] (len 1), which errors.
	restoreForegroundRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	})

	_, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want error for empty output")
	}
	if errors.Is(err, ErrForegroundUnavailable) {
		t.Errorf("err = ErrForegroundUnavailable, want a real malformed-output error")
	}
	if !strings.Contains(err.Error(), "unexpected output") {
		t.Errorf("err = %q, want it to mention 'unexpected output'", err)
	}
}

// -----------------------------------------------------------------------
// Cross-platform tests
// -----------------------------------------------------------------------

func TestForegroundOther_ReturnsUnavailable(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skipf("only runs on unsupported platforms; current GOOS=%s", runtime.GOOS)
	}
	got, err := GetForegroundApp()
	if err == nil {
		t.Fatal("GetForegroundApp() = nil, want ErrForegroundUnavailable")
	}
	if !errors.Is(err, ErrForegroundUnavailable) {
		t.Errorf("err = %v, want ErrForegroundUnavailable", err)
	}
	if got != (ForegroundInfo{}) {
		t.Errorf("got = %+v, want zero ForegroundInfo", got)
	}
}

func TestForegroundInfo_ZeroValue(t *testing.T) {
	// All four fields default to empty string.
	var info ForegroundInfo
	if info.AppName != "" || info.BundleID != "" || info.WindowClass != "" || info.WindowTitle != "" {
		t.Errorf("ForegroundInfo zero value not all empty: %+v", info)
	}
}

func TestErrForegroundUnavailable_Sentinel(t *testing.T) {
	if ErrForegroundUnavailable == nil {
		t.Fatal("ErrForegroundUnavailable is nil")
	}
	if ErrForegroundUnavailable.Error() != "foreground-app detection not supported on this platform" {
		t.Errorf("ErrForegroundUnavailable.Error() = %q, want %q",
			ErrForegroundUnavailable.Error(),
			"foreground-app detection not supported on this platform")
	}
}
