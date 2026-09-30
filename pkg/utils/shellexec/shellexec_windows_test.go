//go:build windows

package shellexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPath_IgnoresMSYSStyleSHELL(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/bash")
	if got := Path(); got == "/usr/bin/bash" {
		t.Fatalf("Path() returned the unresolvable MSYS path %q", got)
	}
}

func TestPath_HonorsRealSHELL(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "mysh.exe")
	if err := os.WriteFile(fake, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", fake)
	if got := Path(); got != fake {
		t.Fatalf("Path() = %q, want %q", got, fake)
	}
}

func TestPath_NeverWSLLauncher(t *testing.T) {
	wsl := filepath.Join(os.Getenv("SystemRoot"), "System32", "bash.exe")
	t.Setenv("SHELL", wsl)
	if got := Path(); strings.EqualFold(got, wsl) {
		t.Fatalf("Path() chose the WSL launcher %q", got)
	}
}

func TestCommand_CmdFallbackPassesCommandVerbatim(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("PATH", os.Getenv("SystemRoot")+`\System32`)
	t.Setenv("ProgramW6432", t.TempDir())
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if sh := Path(); sh != "" {
		t.Skipf("a POSIX shell is still reachable (%s)", sh)
	}
	out, err := Command(`echo "a b" && echo done`).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v: %s", err, out)
	}
	if got := strings.ReplaceAll(string(out), "\r\n", "\n"); got != "\"a b\" \ndone\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestCommand_RunsThroughPOSIXShellWhenAvailable(t *testing.T) {
	if Path() == "" {
		t.Skip("no POSIX shell installed")
	}
	out, err := Command(`printf '%s|' "a b" c`).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v: %s", err, out)
	}
	if string(out) != "a b|c|" {
		t.Fatalf("output = %q", out)
	}
}

func TestCommandContext_CancelKillsWholeTree(t *testing.T) {
	if Path() == "" {
		t.Skip("no POSIX shell installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = CommandContext(ctx, "sleep 20").CombinedOutput()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancelled command returned after %v; the shell's descendants outlived the timeout", elapsed)
	}
}
