package computer_use

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
)

// stubExec replaces commandRunner/haveExec for a test and restores them after.
func stubExec(t *testing.T, available map[string]bool, capture *[][]string) {
	t.Helper()
	prevRun, prevHave := commandRunner, haveExec
	haveExec = func(name string) bool { return available[name] }
	commandRunner = func(name string, args ...string) ([]byte, error) {
		if capture != nil {
			*capture = append(*capture, append([]string{name}, args...))
		}
		return nil, nil
	}
	t.Cleanup(func() { commandRunner, haveExec = prevRun, prevHave })
}

func TestDetectTools_Unsupported(t *testing.T) {
	if _, _, err := detectTools("plan9"); err == nil {
		t.Error("expected error for unsupported OS")
	}
}

func TestDetectTools_LinuxMissingXdotool(t *testing.T) {
	stubExec(t, map[string]bool{"scrot": true}, nil)
	if _, _, err := detectTools("linux"); err == nil || !strings.Contains(err.Error(), "xdotool") {
		t.Errorf("expected xdotool error, got %v", err)
	}
}

func TestDetectTools_LinuxOK(t *testing.T) {
	stubExec(t, map[string]bool{"xdotool": true, "scrot": true}, nil)
	cap, cli, err := detectTools("linux")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cap != "scrot" || cli != "xdotool" {
		t.Errorf("got cap=%q cli=%q", cap, cli)
	}
}

func TestDetectTools_DarwinMissingCliclick(t *testing.T) {
	stubExec(t, map[string]bool{"screencapture": true}, nil)
	if _, _, err := detectTools("darwin"); err == nil || !strings.Contains(err.Error(), "cliclick") {
		t.Errorf("expected cliclick error, got %v", err)
	}
}

func TestCheckPlatformSupport_ReportsReason(t *testing.T) {
	stubExec(t, map[string]bool{}, nil) // nothing available
	got := CheckPlatformSupport()
	// On the test host the real GOOS is used; either it's supported (tools
	// stubbed false → unsupported) or unsupported. Either way Reason must be
	// set when not supported.
	if !got.Supported && got.Reason == "" {
		t.Error("unsupported support should carry a reason")
	}
}

func TestSubprocessBackend_MouseClickArgv(t *testing.T) {
	var calls [][]string
	stubExec(t, map[string]bool{"xdotool": true}, &calls)
	b := &subprocessBackend{os: "linux", cliTool: "xdotool", capTool: "scrot", tmpDir: t.TempDir()}
	if err := b.MouseClick(10, 20, MouseLeft, false); err != nil {
		t.Fatalf("MouseClick: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	want := []string{"xdotool", "mousemove", "10", "20", "click", "1"}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", calls[0], want)
	}
}

func TestSubprocessBackend_DoubleClickRepeats(t *testing.T) {
	var calls [][]string
	stubExec(t, map[string]bool{"xdotool": true}, &calls)
	b := &subprocessBackend{os: "linux", cliTool: "xdotool", capTool: "scrot", tmpDir: t.TempDir()}
	if err := b.MouseClick(1, 2, MouseLeft, true); err != nil {
		t.Fatalf("MouseClick: %v", err)
	}
	if !strings.Contains(strings.Join(calls[0], " "), "--repeat 2") {
		t.Errorf("double-click should repeat: %v", calls[0])
	}
}

func TestNormalizeKeyXdotool(t *testing.T) {
	cases := map[string]string{
		"cmd+space":    "super+space",
		"enter":        "Return",
		"ctrl+shift+t": "ctrl+shift+t",
		"escape":       "Escape",
	}
	for in, want := range cases {
		if got := normalizeKeyXdotool(in); got != want {
			t.Errorf("normalizeKeyXdotool(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSubprocessBackend_ScreenshotReadsAndCrops(t *testing.T) {
	// Build a 4x4 image and have the fake capture tool write it to the path.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	prevRun, prevHave := commandRunner, haveExec
	haveExec = func(string) bool { return true }
	commandRunner = func(name string, args ...string) ([]byte, error) {
		// Last arg is the output path for both scrot and screencapture forms.
		path := args[len(args)-1]
		return nil, os.WriteFile(path, buf.Bytes(), 0o600)
	}
	t.Cleanup(func() { commandRunner, haveExec = prevRun, prevHave })

	b := &subprocessBackend{os: "linux", cliTool: "xdotool", capTool: "scrot", tmpDir: t.TempDir()}

	// Full screen → 4x4.
	_, dims, err := b.Screenshot(nil)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if dims.Width != 4 || dims.Height != 4 {
		t.Errorf("full dims = %+v, want 4x4", dims)
	}

	// Region crop → 2x2.
	data, dims, err := b.Screenshot(&Rect{X: 1, Y: 1, Width: 2, Height: 2})
	if err != nil {
		t.Fatalf("Screenshot region: %v", err)
	}
	if dims.Width != 2 || dims.Height != 2 {
		t.Errorf("cropped dims = %+v, want 2x2", dims)
	}
	if cfg, derr := png.DecodeConfig(bytes.NewReader(data)); derr != nil || cfg.Width != 2 {
		t.Errorf("cropped png invalid: cfg=%+v err=%v", cfg, derr)
	}
}
