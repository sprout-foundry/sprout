//go:build darwin

package computer_use

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/ebitengine/purego"
)

func newPlatformWatcher(keys []string) ChordWatcher {
	return &pollingChordWatcher{keys: keys, open: openQuartzKeyReader}
}

// Reading the keyboard state of other apps' key presses needs the Input
// Monitoring permission; without it CGEventSourceKeyState reports every key
// as up.
var errInputMonitoringDenied = errors.New("panic-key chord needs the Input Monitoring permission — allow sprout (or the terminal running it) in System Settings → Privacy & Security → Input Monitoring, then restart sprout")

const hidSystemState = 1

var quartz struct {
	once sync.Once
	err  error

	keyState        func(stateID int32, key uint16) bool
	preflightListen func() bool
	requestListen   func() bool
}

func loadQuartz() error {
	quartz.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			quartz.err = fmt.Errorf("loading CoreGraphics: %w", err)
			return
		}
		purego.RegisterLibFunc(&quartz.keyState, lib, "CGEventSourceKeyState")
		purego.RegisterLibFunc(&quartz.preflightListen, lib, "CGPreflightListenEventAccess")
		purego.RegisterLibFunc(&quartz.requestListen, lib, "CGRequestListenEventAccess")
	})
	return quartz.err
}

type quartzKeyReader struct{}

func openQuartzKeyReader() (keyReader, error) {
	if err := loadQuartz(); err != nil {
		return nil, err
	}
	if !quartz.preflightListen() {
		// Adds sprout to the Input Monitoring list (and shows the system
		// prompt once), so the user only has to flip the switch.
		quartz.requestListen()
		return nil, errInputMonitoringDenied
	}
	return quartzKeyReader{}, nil
}

func (quartzKeyReader) resolve(token string) ([]int, bool) {
	codes, ok := macKeyCodes[canonicalChordKey(token)]
	return codes, ok
}

func (quartzKeyReader) held() (func(int) bool, error) {
	return func(code int) bool {
		return code >= 0 && code <= math.MaxUint16 && quartz.keyState(hidSystemState, uint16(code))
	}, nil
}

func (quartzKeyReader) close() {}

// Virtual key codes from Carbon's Events.h (kVK_*); they name physical
// positions on an ANSI keyboard.
var macKeyCodes = map[string][]int{
	"ctrl": {59, 62}, "shift": {56, 60}, "alt": {58, 61}, "cmd": {55, 54},
	"escape": {53}, "space": {49}, "enter": {36}, "tab": {48}, "backspace": {51}, "delete": {117},
	"a": {0}, "s": {1}, "d": {2}, "f": {3}, "h": {4}, "g": {5}, "z": {6}, "x": {7}, "c": {8}, "v": {9},
	"b": {11}, "q": {12}, "w": {13}, "e": {14}, "r": {15}, "y": {16}, "t": {17},
	"1": {18}, "2": {19}, "3": {20}, "4": {21}, "6": {22}, "5": {23}, "9": {25}, "7": {26}, "8": {28}, "0": {29},
	"o": {31}, "u": {32}, "i": {34}, "p": {35}, "l": {37}, "j": {38}, "k": {40}, "n": {45}, "m": {46},
	"f1": {122}, "f2": {120}, "f3": {99}, "f4": {118}, "f5": {96}, "f6": {97},
	"f7": {98}, "f8": {100}, "f9": {101}, "f10": {109}, "f11": {103}, "f12": {111},
}
