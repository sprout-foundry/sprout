//go:build !js

package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("SPROUT_SKIP_GIT_LEAK_CHECK") != "" {
		os.Exit(m.Run())
	}
	wd, err := os.Getwd()
	if err != nil || !inRepoPkg(wd) {
		os.Exit(m.Run())
	}
	beforeStatus, beforeHead, err := gitSnapshot(wd)
	if err != nil {
		os.Exit(m.Run())
	}
	code := m.Run()
	afterStatus, afterHead, err := gitSnapshot(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git leak check: post-run snapshot failed: %v\n", err)
		os.Exit(1)
	}
	if beforeStatus != afterStatus {
		fmt.Fprintln(os.Stderr, "GIT LEAK: git status --porcelain changed during test run:")
		fmt.Fprintln(os.Stderr, "--- before ---")
		fmt.Fprintln(os.Stderr, beforeStatus)
		fmt.Fprintln(os.Stderr, "--- after ---")
		fmt.Fprintln(os.Stderr, afterStatus)
		os.Exit(1)
	}
	if beforeHead != afterHead {
		fmt.Fprintf(os.Stderr, "GIT LEAK: HEAD changed during test run: %s -> %s\n", beforeHead, afterHead)
		os.Exit(1)
	}
	os.Exit(code)
}

func inRepoPkg(dir string) bool {
	if !strings.HasSuffix(dir, "pkg"+string(os.PathSeparator)+"workflow") {
		return false
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func gitSnapshot(dir string) (status string, head string, err error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	statusOut, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	headOut, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	return string(statusOut), strings.TrimSpace(string(headOut)), nil
}
