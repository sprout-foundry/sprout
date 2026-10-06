package wasmshell

import (
	"strings"
	"testing"
)

// ─── gh command ──────────────────────────────────────────────────────────

func TestGh_UnknownSubcommandEscalates(t *testing.T) {
	for _, sub := range []string{"release create", "gist create", "api /user"} {
		r := ParseAndExecute("gh " + sub)
		if r.ExitCode != 127 {
			t.Errorf("gh %s exit = %d, want 127 (unknown subcommands escalate)", sub, r.ExitCode)
		}
	}
}

func TestGh_NoExecutor(t *testing.T) {
	RegisterGhExecutor(nil)
	defer RegisterGhExecutor(nil)

	r := ParseAndExecute("gh pr list")
	if r.ExitCode != 127 {
		t.Errorf("exit = %d, want 127 when no gh executor is installed", r.ExitCode)
	}
	if !strings.Contains(r.Stderr, "not available") {
		t.Errorf("stderr = %q", r.Stderr)
	}
}

func TestGh_SubcommandWithExecutor(t *testing.T) {
	var seenSub string
	var seenArgs []string
	RegisterGhExecutor(func(subcommand string, args []string) CmdResult {
		seenSub = subcommand
		seenArgs = args
		return CmdResult{Stdout: "#1 thing\n", Stderr: "", ExitCode: 0}
	})
	defer RegisterGhExecutor(nil)

	r := ParseAndExecute("gh pr checkout 7")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d stderr = %q", r.ExitCode, r.Stderr)
	}
	if seenSub != "pr checkout" {
		t.Errorf("executor saw subcommand %q, want 'pr checkout'", seenSub)
	}
	if len(seenArgs) != 1 || seenArgs[0] != "7" {
		t.Errorf("executor saw args %v, want [7]", seenArgs)
	}
	if r.Stdout != "#1 thing\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestGh_SingleWordSubcommand(t *testing.T) {
	var seenSub string
	RegisterGhExecutor(func(subcommand string, args []string) CmdResult {
		seenSub = subcommand
		return CmdResult{Stdout: "", Stderr: "", ExitCode: 0}
	})
	defer RegisterGhExecutor(nil)

	ParseAndExecute("gh auth status")
	if seenSub != "auth status" {
		t.Errorf("executor saw subcommand %q, want 'auth status'", seenSub)
	}
}

func TestGh_HelpFlag(t *testing.T) {
	r := ParseAndExecute("gh --help")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "gh pr create") {
		t.Errorf("help text missing pr create: %q", r.Stdout)
	}
}
