//go:build !unix || js

package console

import "golang.org/x/term"

// On platforms without a Unix-like termios API (Windows, js/wasm),
// fall back to term.MakeRaw. The OPOST-staircase issue that motivated
// the dedicated steer-mode helpers doesn't manifest the same way here
// because Windows terminals handle CR/LF differently, and js doesn't
// run the CLI REPL at all.

type steerTermiosState struct {
	saved *term.State
}

func enterSteerMode(fd int) (*steerTermiosState, error) {
	st, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &steerTermiosState{saved: st}, nil
}

func exitSteerMode(fd int, st *steerTermiosState) error {
	if st == nil || st.saved == nil {
		return nil
	}
	return term.Restore(fd, st.saved)
}
