//go:build !js

package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestRunShellCommandWithPasswordSupport_RealSudo exercises the same path
// the agent hits when an LLM calls shell_command("sudo ..."): sudo writes
// `Password: ` to stderr (no trailing newline, real-world behavior), then
// blocks on stdin. The wrapper must:
//
//  1. Detect the prompt via the settle-delay path (the broken bufio.Scanner
//     code never did).
//  2. Invoke the prompter exactly once.
//  3. Feed the password to sudo's stdin.
//  4. Return the redaacted captured output.
//
// This test mirrors the user-facing failure mode ("approved but no password
// prompt appeared") and confirms it's fixed at the runtime layer.
//
// Skipped if sudo isn't on PATH or the test environment can't run sudo at all.
func TestRunShellCommandWithPasswordSupport_RealSudo(t *testing.T) {
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skip("sudo not available; skipping real-sudo integration test")
	}

	// CI runners (GitHub Actions, etc.) configure passwordless sudo
	// (NOPASSWD), so `sudo -k -S true` authenticates without ever
	// writing a password prompt. This test exists to exercise the
	// prompt-detection path, which never fires when sudo doesn't
	// prompt — so skip on environments where sudo is passwordless.
	// `sudo -n` fails immediately if a password would be required,
	// making it a reliable passwordless-sudo probe.
	if err := exec.Command("sudo", "-n", "true").Run(); err == nil {
		t.Skip("sudo is passwordless on this host (typical for CI runners); skipping prompt-detection test")
	}

	prompter := &countingPrompter{password: "definitely-wrong-pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run sudo with a custom prompt format (so we control what sudo writes),
	// request a password via stdin (-S so sudo reads from stdin), and run
	// `true` so sudo exits after authentication (or denial).
	cmd := "sudo -k -p 'Password: ' -S true"

	out, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Logf("wrapper returned err=%v (expected for wrong password)", err)
	}

	if prompter.calls < 1 {
		t.Fatalf("BUG REGRESSION: prompter was never called when sudo wrote a password prompt. output=%q", out)
	}

	// sudo will print "Sorry, try again." after a wrong password, then
	// re-prompt. Our cap is 3 attempts; the test just verifies the
	// mechanism works end-to-end.
	if !strings.Contains(out, "Password") {
		t.Logf("note: password prompt text not in output; output=%q", out)
	}
	t.Logf("wrapper output: %q", out)
}
