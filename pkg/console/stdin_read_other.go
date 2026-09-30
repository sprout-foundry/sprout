//go:build !windows

package console

import "os"

// stdinRead reads keyboard input; see stdin_read_windows.go for why
// Windows needs its own implementation.
func stdinRead(p []byte) (int, error) {
	return os.Stdin.Read(p)
}
