//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const sandboxExec = "/usr/bin/sandbox-exec"

var (
	detectOnce sync.Once
	detected   Capability
)

func detect() Capability {
	detectOnce.Do(func() { detected = probeSeatbelt() })
	return detected
}

func probeSeatbelt() Capability {
	c := Capability{Name: "seatbelt"}
	if !exists(sandboxExec) {
		c.Detail = "sandbox-exec is missing, so native mode cannot be sandboxed on this Mac"
		return c
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sandboxExec, "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
	if err != nil {
		c.Detail = fmt.Sprintf("the macOS sandbox refused a test profile: %s", firstLine(out, err))
		return c
	}
	c.Available = true
	return c
}

func commandContext(ctx context.Context, p Policy, name string, args ...string) (*exec.Cmd, error) {
	if c := detect(); !c.Available {
		return nil, fmt.Errorf("native sandbox unavailable: %s", c.Detail)
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return nil, fmt.Errorf("invalid command name %q", name)
	}
	profile, err := seatbeltProfile(p, darwinSystemDirs())
	if err != nil {
		return nil, err
	}
	argv := append([]string{"-p", profile, name}, args...)
	return exec.CommandContext(ctx, sandboxExec, argv...), nil //nolint:gosec // G204: sandbox-exec with a generated profile wrapping the caller's command
}

var (
	userDirsOnce sync.Once
	userDirs     darwinUserDirs
)

// darwinUserDirs are the per-user confstr directories under
// /private/var/folders.
type darwinUserDirs struct {
	Temp  string
	Cache string
}

func darwinSystemDirs() darwinUserDirs {
	userDirsOnce.Do(func() {
		userDirs.Temp = getconf("DARWIN_USER_TEMP_DIR")
		userDirs.Cache = getconf("DARWIN_USER_CACHE_DIR")
	})
	return userDirs
}

func getconf(name string) string {
	out, err := exec.Command("/usr/bin/getconf", name).Output() //nolint:gosec // G204: fixed getconf variable names
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
