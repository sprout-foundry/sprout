//go:build windows

package console

import (
	"os"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows console reads cannot be interrupted: a goroutine parked in
// ReadConsoleW stays there until a key arrives, even after its owner has
// moved on, and then steals that key from whoever reads next. Readers in
// this package therefore call ReadConsoleW only once a key is known to be
// waiting (waitForStdinReadable), and decode UTF-16 themselves so bytes
// left over from a short read stay visible to that readiness check rather
// than hiding in os.File's private buffer.
var conIn consoleInput

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procPeekConsoleInputW = kernel32.NewProc("PeekConsoleInputW")
	procReadConsoleInputW = kernel32.NewProc("ReadConsoleInputW")
)

const keyEventType = 0x0001

// inputRecord mirrors INPUT_RECORD; Event holds the KEY_EVENT_RECORD for
// key events (bKeyDown at 0, UnicodeChar at 10).
type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

func (r *inputRecord) isCharKeyDown() bool {
	if r.EventType != keyEventType {
		return false
	}
	keyDown := *(*uint32)(unsafe.Pointer(&r.Event[0])) != 0 //nolint:gosec // G103: mirrors the fixed-size INPUT_RECORD layout
	char := *(*uint16)(unsafe.Pointer(&r.Event[10]))        //nolint:gosec // G103: mirrors the fixed-size INPUT_RECORD layout
	return keyDown && char != 0
}

type consoleInput struct {
	mu        sync.Mutex
	pending   []byte
	surrogate uint16 // high surrogate carried over from the previous read
}

func stdinConsoleHandle() (windows.Handle, bool) {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	return h, windows.GetConsoleMode(h, &mode) == nil
}

// stdinRead reads keyboard input. On a console it decodes ReadConsoleW
// itself (see conIn); redirected stdin falls through to os.Stdin.
func stdinRead(p []byte) (int, error) {
	h, ok := stdinConsoleHandle()
	if !ok {
		return os.Stdin.Read(p)
	}
	return conIn.read(h, p)
}

func (c *consoleInput) read(h windows.Handle, p []byte) (int, error) {
	if n := c.take(p); n > 0 {
		return n, nil
	}
	for {
		var units [512]uint16
		var got uint32
		if err := windows.ReadConsole(h, &units[0], uint32(len(units)), &got, nil); err != nil {
			return 0, err
		}
		c.mu.Lock()
		c.decodeLocked(units[:got])
		c.mu.Unlock()
		if n := c.take(p); n > 0 {
			return n, nil
		}
	}
}

func (c *consoleInput) take(p []byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n
}

func (c *consoleInput) hasPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending) > 0
}

func (c *consoleInput) decodeLocked(units []uint16) {
	if c.surrogate != 0 {
		units = append([]uint16{c.surrogate}, units...)
		c.surrogate = 0
	}
	if n := len(units); n > 0 && units[n-1] >= 0xD800 && units[n-1] < 0xDC00 {
		c.surrogate = units[n-1]
		units = units[:n-1]
	}
	for _, r := range utf16.Decode(units) {
		c.pending = utf8.AppendRune(c.pending, r)
	}
}

// waitForStdinReadable reports whether a keystroke can be read without
// blocking, waiting up to timeout for one. The console handle is signaled
// by any input record (key releases, focus and resize events), none of
// which ReadConsoleW returns, so those are drained here instead of letting
// a subsequent read block on them.
func waitForStdinReadable(_ int, timeout time.Duration) bool {
	h, ok := stdinConsoleHandle()
	if !ok || conIn.hasPending() {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		ready, err := charKeyWaiting(h)
		if err != nil || ready {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		ms := uint32((remaining + time.Millisecond - 1) / time.Millisecond) //nolint:gosec // G115: timeout ms is far below the uint32 ceiling
		ev, err := windows.WaitForSingleObject(h, ms)
		if err != nil {
			return true
		}
		if ev == uint32(windows.WAIT_TIMEOUT) {
			return false
		}
	}
}

// charKeyWaiting peeks the console input queue. It returns true when a
// key-down carrying a character is queued; otherwise it discards the
// queued records, none of which a read would return.
func charKeyWaiting(h windows.Handle) (bool, error) {
	var recs [64]inputRecord
	var n uint32
	r, _, err := procPeekConsoleInputW.Call(uintptr(h), uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)), uintptr(unsafe.Pointer(&n))) //nolint:gosec // G103: console input-queue peek via a lazy system call
	if r == 0 {
		return false, err
	}
	for i := uint32(0); i < n; i++ {
		if recs[i].isCharKeyDown() {
			return true, nil
		}
	}
	if n > 0 {
		var removed uint32
		// Best-effort drain of queued non-key input records (focus, resize).
		_, _, _ = procReadConsoleInputW.Call(uintptr(h), uintptr(unsafe.Pointer(&recs[0])), uintptr(n), uintptr(unsafe.Pointer(&removed))) //nolint:gosec // G103: audited console input-queue drain
	}
	return false, nil
}

// startupModes holds the console modes the parent shell left in place, so
// RestoreTerminal can hand the console back the way it was found.
var startupModes = captureConsoleModes()

type consoleModes struct {
	in, out         windows.Handle
	inMode          uint32
	outMode         uint32
	haveIn, haveOut bool
}

func captureConsoleModes() consoleModes {
	var m consoleModes
	m.in = windows.Handle(os.Stdin.Fd())
	m.out = windows.Handle(os.Stdout.Fd())
	m.haveIn = windows.GetConsoleMode(m.in, &m.inMode) == nil
	m.haveOut = windows.GetConsoleMode(m.out, &m.outMode) == nil
	return m
}

func restoreStartupConsoleModes() {
	if startupModes.haveIn {
		_ = windows.SetConsoleMode(startupModes.in, startupModes.inMode)
	}
	if startupModes.haveOut {
		_ = windows.SetConsoleMode(startupModes.out, startupModes.outMode)
	}
}

// init turns on VT processing for the console output handles. Sprout's
// footer, colors and cursor movement are all escape sequences; a classic
// console window can start with processing off (Windows Terminal and
// pwsh usually leave it on), which prints them as literal "←[" text.
// startupModes has already captured the original modes for
// RestoreTerminal. If VT processing cannot be enabled the process-wide
// markVTUnsupported flips so color and cursor control fall back to plain
// output instead of emitting raw escapes.
func init() {
	for _, h := range []windows.Handle{windows.Handle(os.Stdout.Fd()), windows.Handle(os.Stderr.Fd())} {
		var mode uint32
		if windows.GetConsoleMode(h, &mode) != nil {
			continue
		}
		want := mode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if want == mode {
			continue
		}
		if err := windows.SetConsoleMode(h, want); err != nil {
			markVTUnsupported()
		}
	}
}
