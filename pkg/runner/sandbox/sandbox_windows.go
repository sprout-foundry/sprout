//go:build windows

package sandbox

import (
	"context"
	"errors"
	"os/exec"
)

func detect() Capability {
	return Capability{
		Name:   "restricted-token",
		Weak:   true,
		Detail: "native sandboxing on Windows is not implemented yet; use container or bare-metal mode",
	}
}

func commandContext(context.Context, Policy, string, ...string) (*exec.Cmd, error) {
	return nil, errors.New(detect().Detail)
}
