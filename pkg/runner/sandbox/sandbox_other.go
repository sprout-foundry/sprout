//go:build !darwin && !linux && !windows

package sandbox

import (
	"context"
	"errors"
	"os/exec"
)

func detect() Capability {
	return Capability{
		Name:   "none",
		Detail: "native sandboxing is not supported on this operating system; use container or bare-metal mode",
	}
}

func commandContext(context.Context, Policy, string, ...string) (*exec.Cmd, error) {
	return nil, errors.New(detect().Detail)
}
