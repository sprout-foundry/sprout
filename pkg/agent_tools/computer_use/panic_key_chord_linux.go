//go:build linux

package computer_use

import (
	"errors"
	"fmt"
	"os"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func newPlatformWatcher(keys []string) ChordWatcher {
	return &pollingChordWatcher{keys: keys, open: openX11KeyReader}
}

// x11KeyReader reads the keyboard through the X server, so it sees keys on
// X11 sessions and on XWayland; a pure Wayland compositor gives no client a
// global keyboard view.
type x11KeyReader struct {
	conn     *xgb.Conn
	byKeysym map[xproto.Keysym][]int
}

func openX11KeyReader() (keyReader, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, errors.New("panic-key chord needs an X11 display (DISPLAY is unset)")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connecting to the X server: %w", err)
	}
	setup := xproto.Setup(conn)
	first, last := setup.MinKeycode, setup.MaxKeycode
	mapping, err := xproto.GetKeyboardMapping(conn, first, byte(last-first+1)).Reply()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading the keyboard mapping: %w", err)
	}
	byKeysym := make(map[xproto.Keysym][]int)
	per := int(mapping.KeysymsPerKeycode)
	for i := 0; per > 0 && i*per < len(mapping.Keysyms); i++ {
		code := int(first) + i
		for _, sym := range mapping.Keysyms[i*per : (i+1)*per] {
			if sym != 0 {
				byKeysym[sym] = append(byKeysym[sym], code)
			}
		}
	}
	return &x11KeyReader{conn: conn, byKeysym: byKeysym}, nil
}

func (r *x11KeyReader) resolve(token string) ([]int, bool) {
	syms, ok := x11Keysyms[canonicalChordKey(token)]
	if !ok {
		return nil, false
	}
	var codes []int
	for _, sym := range syms {
		codes = append(codes, r.byKeysym[sym]...)
	}
	return codes, len(codes) > 0
}

func (r *x11KeyReader) held() (func(int) bool, error) {
	keymap, err := xproto.QueryKeymap(r.conn).Reply()
	if err != nil {
		return nil, err
	}
	return func(code int) bool {
		return code >= 0 && code < 256 && keymap.Keys[code/8]&(1<<(code%8)) != 0
	}, nil
}

func (r *x11KeyReader) close() { r.conn.Close() }

// Keysyms from X11's keysymdef.h.
var x11Keysyms = map[string][]xproto.Keysym{
	"ctrl":      {0xffe3, 0xffe4},
	"shift":     {0xffe1, 0xffe2},
	"alt":       {0xffe9, 0xffea, 0xffe7, 0xffe8},
	"cmd":       {0xffeb, 0xffec},
	"escape":    {0xff1b},
	"space":     {0x20},
	"enter":     {0xff0d},
	"tab":       {0xff09},
	"backspace": {0xff08},
	"delete":    {0xffff},
}

func init() {
	for c := 'a'; c <= 'z'; c++ {
		x11Keysyms[string(c)] = []xproto.Keysym{xproto.Keysym(c)}
	}
	for c := '0'; c <= '9'; c++ {
		x11Keysyms[string(c)] = []xproto.Keysym{xproto.Keysym(c)}
	}
	for n := 1; n <= 12; n++ {
		x11Keysyms[fmt.Sprintf("f%d", n)] = []xproto.Keysym{xproto.Keysym(0xffbe + n - 1)}
	}
}
