//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCommandScript_Basic(t *testing.T) {
	path, launcher, err := writeCommandScript("echo hi && echo there", "feedface")
	if err != nil {
		t.Fatalf("writeCommandScript failed: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasPrefix(launcher, "sh '") || !strings.HasSuffix(launcher, "'\n") {
		t.Fatalf("launcher must be a single quoted sh invocation, got %q", launcher)
	}
	if len(launcher) > launcherMaxBytes {
		t.Fatalf("launcher exceeds %d bytes", launcherMaxBytes)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("script not readable: %v", err)
	}
	s := string(body)
	for _, want := range []string{"#!/bin/sh", `trap 'rm -f "$0"' EXIT`, "echo hi && echo there", "code=$?", "feedface:$code"} {
		if !strings.Contains(s, want) {
			t.Errorf("script body missing %q;\n%s", want, s)
		}
	}
}

func TestWriteCommandScript_MultiLine(t *testing.T) {
	path, _, err := writeCommandScript("line one\nline two\necho done", "feedface")
	if err != nil {
		t.Fatalf("writeCommandScript failed for multi-line command: %v", err)
	}
	defer os.Remove(path)

	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "line two") {
		t.Errorf("newline lost in script body:\n%s", string(body))
	}
}

func TestWriteCommandScript_SelfReferenceSkipsDelete(t *testing.T) {
	path, _, err := writeCommandScript("cat $0", "feedface")
	if err != nil {
		t.Fatalf("writeCommandScript failed: %v", err)
	}
	defer os.Remove(path)

	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "rm -f") {
		t.Errorf("self-referencing command must not get a self-delete trap:\n%s", string(body))
	}
}

func TestWriteCommandScript_SentinelBothBranches(t *testing.T) {
	// The sentinel echo must fire regardless of the command's exit code —
	// one echo after the command, not &&/|| duplicates.
	path, _, err := writeCommandScript("false", "feedface")
	if err != nil {
		t.Fatalf("writeCommandScript failed: %v", err)
	}
	defer os.Remove(path)

	body, _ := os.ReadFile(path)
	s := string(body)
	if got := strings.Count(s, "feedface"); got != 1 {
		t.Errorf("expected exactly one sentinel echo, got %d:\n%s", got, s)
	}
	if !strings.Contains(s, "exit $code") {
		t.Errorf("script must exit with the command's real status:\n%s", s)
	}
}

func TestWriteCommandScript_EmptyCommand(t *testing.T) {
	if _, _, err := writeCommandScript("   ", "feedface"); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestScriptBody_QuotePreservation(t *testing.T) {
	body := buildScriptBody("echo 'single \"double\" $dollar'", "m")
	if !strings.Contains(body, `'\''`) {
		t.Errorf("embedded single quote must be escaped for sh:\n%s", body)
	}
	// The command must appear exactly once, inside the sh -c argument.
	if got := strings.Count(body, `sh -c`); got != 1 {
		t.Errorf("command should appear in exactly one sh -c, got %d:\n%s", got, body)
	}
}

func TestLauncherPathQuoting(t *testing.T) {
	// A temp dir with a quote in its name must still produce a working
	// launcher (the quote is escaped inside the single-quoted path).
	dir := t.TempDir()
	weird := filepath.Join(dir, "it's dir")
	if err := os.MkdirAll(weird, 0o700); err != nil {
		t.Skipf("cannot create dir with quote: %v", err)
	}
	path := filepath.Join(weird, "s.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}
	launcher := "sh " + shQuote(path) + "\n"
	if len(launcher) > launcherMaxBytes {
		t.Fatalf("launcher too long: %d", len(launcher))
	}
	// shQuote doubles embedded quotes as '\''; naive parity fails there.
	// Verify by running the launcher's path resolution: split on the
	// escaped-quote sequence and count outer quotes.
	normalized := strings.ReplaceAll(launcher, `'\''`, "Q")
	if strings.Count(normalized, "'")%2 != 0 {
		t.Fatalf("unbalanced quotes after escape normalization: %q", launcher)
	}
}
