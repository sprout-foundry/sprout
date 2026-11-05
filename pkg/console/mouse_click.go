package console

// mouse_click.go — click-to-place-caret in the CLI input, opt-in
// (mouse_click_to_position).
//
// Clicks are only reported with the terminal's click tracking (?1000h),
// which also captures the wheel; SP-166 gave the wheel back to the terminal
// so its scrollback works. The setting trades that for clicks, so it is off
// unless the user turns it on, and every input surface enables the modes
// through mouseInputModes so the choice is honored everywhere.

import (
	"sync/atomic"
)

// mouseClickReportingEnable turns on press/release reporting (and, as a
// side effect no terminal lets us avoid, wheel reporting).
const mouseClickReportingEnable = "\x1b[?1000h"

var mouseClickSource atomic.Pointer[func() bool]

// SetMouseClickToPositionSource registers where the setting is read from.
// Read each time an input surface takes the terminal, so a change applies
// from the next prompt.
func SetMouseClickToPositionSource(fn func() bool) {
	if fn == nil {
		mouseClickSource.Store(nil)
		return
	}
	mouseClickSource.Store(&fn)
}

func mouseClickToPosition() bool {
	if fn := mouseClickSource.Load(); fn != nil {
		return (*fn)()
	}
	return false
}

// mouseInputModes is the mouse-mode sequence an input surface enables: SGR
// coordinates always, click reporting only when the setting is on.
func mouseInputModes() string {
	if mouseClickToPosition() {
		return MouseTrackingSGR + mouseClickReportingEnable
	}
	return MouseTrackingSGR
}

// leftClickAt parses a mouse event and reports a left-button press's
// 1-based screen position.
func leftClickAt(data string) (row, col int, ok bool) {
	ev, err := ParseMouseEvent(data)
	if err != nil || ev == nil || ev.Button != MouseButtonLeft || ev.Kind != MouseEventPress {
		return 0, 0, false
	}
	return ev.Row, ev.Col, true
}
