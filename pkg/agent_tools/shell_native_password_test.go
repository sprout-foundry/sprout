//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
)

// countingPrompter implements PasswordPrompter for testing — tracks call count
// and supports returning errors.
type countingPrompter struct {
	password string
	calls    int
	err      error
}

func (f *countingPrompter) Prompt(_ context.Context, _ string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.password, nil
}

func TestPasswordPromptRegex(t *testing.T) {
	positive := []string{
		"Password:",
		"password:",
		"PASSWORD:",
		"[sudo] password for user:",
		"[sudo] password for root:",
		"Enter passphrase:",
		"Enter PEM pass phrase:",
		"Enter the passphrase:",
		"passphrase for mykey:",
		"password for admin:",
		"  Password:  ",
	}
	negative := []string{
		"Password changed",
		"enter your name:",
		"Please enter password below",
		"Password is required",
		"Your password was reset",
		"Change password",
	}

	for _, tc := range positive {
		t.Run("match:"+tc, func(t *testing.T) {
			if !passwordPromptRe.MatchString(tc) {
				t.Errorf("expected %q to match password prompt regex", tc)
			}
		})
	}

	for _, tc := range negative {
		t.Run("no-match:"+tc, func(t *testing.T) {
			if passwordPromptRe.MatchString(tc) {
				t.Errorf("expected %q to NOT match password prompt regex", tc)
			}
		})
	}
}

func TestRunShellCommandWithPasswordSupport_NoPrompt(t *testing.T) {
	prompter := &countingPrompter{password: "secret123"}
	ctx := context.Background()

	output, err := runShellCommandWithPasswordSupport(ctx, "echo hello", prompter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompter.calls != 0 {
		t.Errorf("prompter should not have been called, got %d calls", prompter.calls)
	}

	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

func TestRunShellCommandWithPasswordSupport_RedactsPassword(t *testing.T) {
	// Simulate a command that prompts for a password and echoes it back.
	// The redaction should replace the password value with [REDACTED].
	prompter := &countingPrompter{password: "mysecret"}
	ctx := context.Background()

	// This command prints a password prompt, reads a line, then echoes it back.
	cmd := `sh -c 'echo "Password:"; read pass; echo "You entered: $pass"'`
	output, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d calls", prompter.calls)
	}

	if strings.Contains(output, "mysecret") {
		t.Errorf("password 'mysecret' should be redacted from output, got: %s", output)
	}

	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %s", output)
	}
}

func TestRunShellCommandWithPasswordSupport_MaxAttempts(t *testing.T) {
	// This command prompts 4 times for a password. We should only respond to 3.
	prompter := &countingPrompter{password: "wrong"}
	ctx := context.Background()

	cmd := `sh -c '
echo "Password:"
read pass1
echo "Password:"
read pass2
echo "Password:"
read pass3
echo "Password:"
read pass4
echo "done"
'`

	output, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	// The command may fail because the 4th read blocks with no input (stdin closed),
	// or it may succeed if sh handles EOF gracefully. Either way, the prompter
	// should have been called at most 3 times.
	_ = output
	_ = err

	if prompter.calls > maxPasswordAttempts {
		t.Errorf("prompter was called %d times, expected at most %d", prompter.calls, maxPasswordAttempts)
	}

	if prompter.calls < 3 {
		// It's acceptable if the command exits early (e.g., read fails on EOF),
		// but we should have at least tried.
		t.Logf("prompter was called %d times (command may have exited early)", prompter.calls)
	}
}

func TestRunShellCommandWithPasswordSupport_ContextCancellation(t *testing.T) {
	prompter := &countingPrompter{password: "secret"}
	ctx, cancel := context.WithCancel(context.Background())

	// A command that sleeps forever
	cmd := "sleep 100"

	// Cancel the context immediately
	cancel()

	_, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}

	if prompter.calls != 0 {
		t.Errorf("prompter should not have been called, got %d calls", prompter.calls)
	}
}

func TestRunShellCommandWithPasswordSupport_ErrNoInteractiveSurface(t *testing.T) {
	// When the prompter returns ErrNoInteractiveSurface, the command should
	// proceed normally (no password sent) and fail with whatever error the
	// command produces.
	prompter := &countingPrompter{err: ErrNoInteractiveSurface}
	ctx := context.Background()

	// A command that prompts for a password — without stdin it will fail.
	cmd := `sh -c 'echo "Password:"; read pass; echo "got: $pass"'`
	_, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	// The command will fail because read gets EOF (no stdin input).
	// We just verify it doesn't panic and the prompter was called once.
	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d calls", prompter.calls)
	}
	// The error is expected — the command can't proceed without a password.
	if err == nil {
		// Actually, read might succeed with empty input from the pipe.
		// That's fine — the important thing is the prompter was called.
	}
}
