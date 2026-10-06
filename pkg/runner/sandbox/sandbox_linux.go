//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	detectOnce sync.Once
	detected   Capability
	bwrapPath  string
)

func detect() Capability {
	detectOnce.Do(func() { detected = probeBwrap() })
	return detected
}

func probeBwrap() Capability {
	c := Capability{Name: "bwrap"}
	path, err := exec.LookPath("bwrap")
	if err != nil {
		c.Detail = "bubblewrap (bwrap) is not installed; install it to sandbox native mode"
		return c
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--ro-bind", "/", "/", "/bin/true").CombinedOutput() //nolint:gosec // G204: probing the bwrap found on PATH
	if err != nil {
		c.Detail = fmt.Sprintf("bubblewrap cannot create a sandbox here (user namespaces may be disabled, or this is a container): %s", firstLine(out, err))
		return c
	}
	bwrapPath = path
	c.Available = true
	return c
}

func commandContext(ctx context.Context, p Policy, name string, args ...string) (*exec.Cmd, error) {
	if c := detect(); !c.Available {
		return nil, fmt.Errorf("native sandbox unavailable: %s", c.Detail)
	}
	bwArgs, err := bwrapArgs(p)
	if err != nil {
		return nil, err
	}
	argv := append(bwArgs, "--", name)
	argv = append(argv, args...)
	return exec.CommandContext(ctx, bwrapPath, argv...), nil //nolint:gosec // G204: bwrap with generated args wrapping the caller's command
}

// bwrapArgs renders p as bubblewrap flags. Mount order matters: later
// mounts shadow earlier ones, so the writable binds come after the fresh
// /tmp (a TempDir under /tmp must survive it) and the deny-read masks come
// last so they win over any overlapping writable bind.
func bwrapArgs(p Policy) ([]string, error) {
	if p.WorkDir == "" {
		return nil, errors.New("policy has no WorkDir")
	}
	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--unshare-pid",
		"--die-with-parent",
		"--new-session",
	}
	if !p.AllowNetwork {
		args = append(args, "--unshare-net")
	}

	work, err := canonicalPath(p.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("workdir: %w", err)
	}
	if !exists(work) {
		return nil, fmt.Errorf("workdir %q does not exist", work)
	}
	writable := []string{work}
	if p.TempDir != "" {
		writable = append(writable, p.TempDir)
	}
	writable = append(writable, p.Writable...)
	seen := map[string]bool{}
	for _, w := range writable {
		c, err := canonicalPath(w)
		if err != nil {
			return nil, fmt.Errorf("writable path: %w", err)
		}
		if seen[c] || !exists(c) {
			continue
		}
		seen[c] = true
		args = append(args, "--bind", c, c)
	}

	seen = map[string]bool{}
	for _, d := range p.DenyRead {
		c, err := canonicalPath(d)
		if err != nil {
			return nil, fmt.Errorf("deny-read path: %w", err)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		info, err := os.Stat(c)
		if err != nil {
			continue
		}
		if info.IsDir() {
			args = append(args, "--tmpfs", c)
		} else {
			args = append(args, "--ro-bind", "/dev/null", c)
		}
	}
	return args, nil
}
