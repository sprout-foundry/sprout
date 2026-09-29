//go:build !windows

package shellexec

import (
	"os"
	"os/exec"
)

// Path returns the POSIX shell used to run commands: $SHELL, else /bin/sh.
func Path() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

func argv(command string) (string, []string) {
	return Path(), []string{"-c", command}
}

func prepare(*exec.Cmd, string) {}
