//go:build (darwin || freebsd || netbsd || openbsd || dragonfly) && !js

package console

import (
	"golang.org/x/sys/unix"
)

// On BSDs (including macOS) the termios ioctls use TIOCGETA / TIOCSETA
// instead of TCGETS / TCSETS. Otherwise the field-clearing logic is
// identical to the Linux/aix/solaris variant in steer_termios_unix.go.
// See that file for the rationale on which flags we touch and which
// we preserve.

type steerTermiosState struct {
	saved unix.Termios
}

func enterSteerMode(fd int) (*steerTermiosState, error) {
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	st := &steerTermiosState{saved: *termios}

	t := *termios
	t.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	t.Iflag &^= unix.ICRNL
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &t); err != nil {
		return nil, err
	}
	return st, nil
}

func exitSteerMode(fd int, st *steerTermiosState) error {
	if st == nil {
		return nil
	}
	saved := st.saved
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, &saved)
}
