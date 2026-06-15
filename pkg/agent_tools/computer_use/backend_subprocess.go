package computer_use

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// subprocessBackend drives the desktop via platform CLI tools (no cgo):
//
//	macOS      → screencapture (screenshot) + cliclick (input)
//	Linux/X11  → scrot|ImageMagick import (screenshot) + xdotool (input)
//
// Wayland and other platforms are unsupported; NewPlatformBackend returns a
// descriptive error there. Region cropping is done in-process (image/png) so we
// don't depend on a capture tool's crop flags being present.
type subprocessBackend struct {
	os      string // runtime.GOOS at construction
	tmpDir  string // scratch dir for screenshots
	cliTool string // input tool: "cliclick" (darwin) or "xdotool" (linux)
	capTool string // capture tool: "screencapture", "scrot", or "import"
}

// commandRunner is overridable in tests so we can assert on the exact argv a
// backend would invoke without touching a real display.
var commandRunner = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (b *subprocessBackend) run(name string, args ...string) error {
	out, err := commandRunner(name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Screenshot captures the full screen to a temp PNG, reads it, and crops to
// region in-process when one is requested.
func (b *subprocessBackend) Screenshot(region *Rect) ([]byte, Size, error) {
	path := filepath.Join(b.tmpDir, "sprout-cu-screenshot.png")
	defer os.Remove(path)

	var args []string
	switch b.capTool {
	case "screencapture":
		args = []string{"-x", "-t", "png", path}
	case "scrot":
		args = []string{"-z", "--overwrite", path}
	case "import":
		args = []string{"-window", "root", path}
	default:
		return nil, Size{}, fmt.Errorf("no screenshot tool available")
	}
	if err := b.run(b.capTool, args...); err != nil {
		return nil, Size{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Size{}, fmt.Errorf("read screenshot: %w", err)
	}

	if region == nil {
		cfg, derr := png.DecodeConfig(bytes.NewReader(data))
		if derr != nil {
			return nil, Size{}, fmt.Errorf("decode screenshot dims: %w", derr)
		}
		return data, Size{Width: cfg.Width, Height: cfg.Height}, nil
	}

	cropped, dims, err := cropPNG(data, *region)
	if err != nil {
		return nil, Size{}, err
	}
	return cropped, dims, nil
}

func (b *subprocessBackend) MouseClick(x, y int, button MouseButton, double bool) error {
	switch b.cliTool {
	case "cliclick":
		verb := map[MouseButton]string{MouseLeft: "c", MouseRight: "rc", MouseMiddle: "c"}[button]
		if double {
			verb = "dc"
		}
		return b.run("cliclick", fmt.Sprintf("%s:%d,%d", verb, x, y))
	case "xdotool":
		btn := map[MouseButton]string{MouseLeft: "1", MouseMiddle: "2", MouseRight: "3"}[button]
		args := []string{"mousemove", strconv.Itoa(x), strconv.Itoa(y), "click"}
		if double {
			args = append(args, "--repeat", "2")
		}
		args = append(args, btn)
		return b.run("xdotool", args...)
	}
	return fmt.Errorf("no input tool available")
}

func (b *subprocessBackend) MouseDrag(from, to Point, button MouseButton) error {
	switch b.cliTool {
	case "cliclick":
		return b.run("cliclick",
			fmt.Sprintf("dd:%d,%d", from.X, from.Y),
			fmt.Sprintf("dm:%d,%d", to.X, to.Y),
			fmt.Sprintf("du:%d,%d", to.X, to.Y),
		)
	case "xdotool":
		btn := map[MouseButton]string{MouseLeft: "1", MouseMiddle: "2", MouseRight: "3"}[button]
		return b.run("xdotool",
			"mousemove", strconv.Itoa(from.X), strconv.Itoa(from.Y),
			"mousedown", btn,
			"mousemove", strconv.Itoa(to.X), strconv.Itoa(to.Y),
			"mouseup", btn,
		)
	}
	return fmt.Errorf("no input tool available")
}

func (b *subprocessBackend) KeyboardType(text string) error {
	switch b.cliTool {
	case "cliclick":
		return b.run("cliclick", "t:"+text)
	case "xdotool":
		return b.run("xdotool", "type", "--clearmodifiers", "--", text)
	}
	return fmt.Errorf("no input tool available")
}

func (b *subprocessBackend) KeyboardPress(key string) error {
	switch b.cliTool {
	case "cliclick":
		return b.pressChordCliclick(key)
	case "xdotool":
		// xdotool understands chords natively (e.g. "ctrl+shift+t", "Return").
		return b.run("xdotool", "key", normalizeKeyXdotool(key))
	}
	return fmt.Errorf("no input tool available")
}

func (b *subprocessBackend) Scroll(dir ScrollDir, amount int, at *Point) error {
	if amount <= 0 {
		amount = 1
	}
	switch b.cliTool {
	case "xdotool":
		btn := map[ScrollDir]string{ScrollUp: "4", ScrollDown: "5", ScrollLeft: "6", ScrollRight: "7"}[dir]
		if at != nil {
			if err := b.run("xdotool", "mousemove", strconv.Itoa(at.X), strconv.Itoa(at.Y)); err != nil {
				return err
			}
		}
		return b.run("xdotool", "click", "--repeat", strconv.Itoa(amount), btn)
	case "cliclick":
		// cliclick has no portable scroll verb; emulate with arrow/page keys.
		key := map[ScrollDir]string{ScrollUp: "arrow-up", ScrollDown: "arrow-down", ScrollLeft: "arrow-left", ScrollRight: "arrow-right"}[dir]
		for i := 0; i < amount; i++ {
			if err := b.run("cliclick", "kp:"+key); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("no input tool available")
}

// pressChordCliclick maps a chord like "cmd+space" to cliclick key-down/up
// around a tap of the final key.
func (b *subprocessBackend) pressChordCliclick(key string) error {
	parts := strings.Split(key, "+")
	if len(parts) == 1 {
		return b.run("cliclick", "kp:"+normalizeKeyCliclick(parts[0]))
	}
	var args []string
	mods := parts[:len(parts)-1]
	final := parts[len(parts)-1]
	for _, m := range mods {
		args = append(args, "kd:"+normalizeModCliclick(m))
	}
	args = append(args, "kp:"+normalizeKeyCliclick(final))
	for i := len(mods) - 1; i >= 0; i-- {
		args = append(args, "ku:"+normalizeModCliclick(mods[i]))
	}
	return b.run("cliclick", args...)
}

func normalizeModCliclick(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "cmd", "command", "meta", "super", "win":
		return "cmd"
	case "ctrl", "control":
		return "ctrl"
	case "alt", "option", "opt":
		return "alt"
	case "shift":
		return "shift"
	default:
		return strings.ToLower(m)
	}
}

func normalizeKeyCliclick(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "enter", "return":
		return "return"
	case "esc", "escape":
		return "esc"
	case "tab":
		return "tab"
	case "space":
		return "space"
	case "delete", "backspace":
		return "delete"
	default:
		return strings.ToLower(k)
	}
}

// normalizeKeyXdotool maps common key names/chords to xdotool's keysym
// vocabulary (e.g. "cmd"→"super", "enter"→"Return").
func normalizeKeyXdotool(key string) string {
	parts := strings.Split(key, "+")
	for i, p := range parts {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "cmd", "command", "meta", "win":
			parts[i] = "super"
		case "ctrl", "control":
			parts[i] = "ctrl"
		case "alt", "option", "opt":
			parts[i] = "alt"
		case "shift":
			parts[i] = "shift"
		case "enter", "return":
			parts[i] = "Return"
		case "esc", "escape":
			parts[i] = "Escape"
		case "tab":
			parts[i] = "Tab"
		case "space":
			parts[i] = "space"
		case "backspace":
			parts[i] = "BackSpace"
		case "delete", "del":
			parts[i] = "Delete"
		default:
			parts[i] = p
		}
	}
	return strings.Join(parts, "+")
}

// cropPNG crops a PNG to r and re-encodes it, returning the cropped bytes and
// dimensions. The region is clamped to the image bounds.
func cropPNG(data []byte, r Rect) ([]byte, Size, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, Size{}, fmt.Errorf("decode for crop: %w", err)
	}
	b := img.Bounds()
	x0 := clamp(r.X, 0, b.Dx())
	y0 := clamp(r.Y, 0, b.Dy())
	x1 := clamp(r.X+r.Width, x0, b.Dx())
	y1 := clamp(r.Y+r.Height, y0, b.Dy())
	rect := image.Rect(b.Min.X+x0, b.Min.Y+y0, b.Min.X+x1, b.Min.Y+y1)

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return nil, Size{}, fmt.Errorf("image type %T does not support cropping", img)
	}
	sub := si.SubImage(rect)
	var buf bytes.Buffer
	if err := png.Encode(&buf, sub); err != nil {
		return nil, Size{}, fmt.Errorf("encode cropped png: %w", err)
	}
	d := sub.Bounds()
	return buf.Bytes(), Size{Width: d.Dx(), Height: d.Dy()}, nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// detectTools resolves the capture + input CLI tools available for the current
// OS, returning a descriptive error when the platform/toolchain is unsupported.
func detectTools(goos string) (capTool, cliTool string, err error) {
	switch goos {
	case "darwin":
		if !haveExec("screencapture") {
			return "", "", fmt.Errorf("macOS: 'screencapture' not found")
		}
		if !haveExec("cliclick") {
			return "", "", fmt.Errorf("macOS: 'cliclick' not found — install with 'brew install cliclick'")
		}
		return "screencapture", "cliclick", nil
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("DISPLAY") == "" {
			return "", "", fmt.Errorf("linux/Wayland is unsupported (synthetic input is blocked); use an X11 session")
		}
		if !haveExec("xdotool") {
			return "", "", fmt.Errorf("linux/X11: 'xdotool' not found — install it (e.g. 'apt install xdotool')")
		}
		switch {
		case haveExec("scrot"):
			return "scrot", "xdotool", nil
		case haveExec("import"):
			return "import", "xdotool", nil
		default:
			return "", "", fmt.Errorf("linux/X11: no screenshot tool found — install 'scrot' or ImageMagick ('import')")
		}
	default:
		return "", "", fmt.Errorf("computer use is not supported on %s (supported: macOS, linux/X11)", goos)
	}
}

// haveExec is overridable in tests.
var haveExec = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var _ = runtime.GOOS // referenced by callers; kept for clarity
