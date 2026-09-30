//go:build !js

package webui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// Pipe sessions back the Windows terminal and the Unix no-PTY fallback. They
// must hand ExecuteCommandAndWait a writable stdin and must not strip real
// output that merely looks like the PTY's echo of the wrapped command.
func TestPipeSessionExecutesAgentCommands(t *testing.T) {
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = shellexec.Path()
		if shell == "" {
			t.Skip("no POSIX shell (Git for Windows) installed")
		}
	}
	tm := newTestTerminalManager(t, t.TempDir())
	session, err := tm.startPipeSession("pipe-exec", shell, nil)
	if err != nil {
		t.Fatalf("startPipeSession: %v", err)
	}
	session.Hidden = true
	tm.mutex.Lock()
	tm.sessions[session.ID] = session
	tm.mutex.Unlock()

	if !session.NoPTY || session.Pty == nil {
		t.Fatalf("pipe session: NoPTY=%v Pty=%v", session.NoPTY, session.Pty)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, code, err := tm.ExecuteCommandAndWait(ctx, session, `echo "/bin/sh -c 'kept'" && false`)
	if err != nil {
		t.Fatalf("ExecuteCommandAndWait: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(output, "/bin/sh -c 'kept'") {
		t.Errorf("output = %q, want it to keep the command's own output", output)
	}
}

func TestCookPipeInput(t *testing.T) {
	s := &TerminalSession{NoPTY: true, ring: newSessRing()}
	sub := s.subscribe()
	defer s.unsubscribe(sub)

	if out := s.cookPipeInputLocked([]byte("lx\x7fs\x1b[A -la")); len(out) != 0 {
		t.Fatalf("partial line leaked to the shell: %q", out)
	}
	if out := string(s.cookPipeInputLocked([]byte("\r"))); out != "ls -la\n" {
		t.Fatalf("Enter produced %q, want %q", out, "ls -la\n")
	}
	if out := s.cookPipeInputLocked([]byte("oops\x03")); len(out) != 0 || len(s.pipeLine) != 0 {
		t.Fatalf("Ctrl+C should discard the line: out=%q line=%q", out, s.pipeLine)
	}

	var echo []byte
	for len(sub.ch) > 0 {
		echo = append(echo, <-sub.ch...)
	}
	if want := "lx\b \bs -la\r\noops^C\r\n"; string(echo) != want {
		t.Errorf("echo = %q, want %q", echo, want)
	}
}

func TestPipeSessionInteractiveEnterRunsCommand(t *testing.T) {
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = shellexec.Path()
		if shell == "" {
			t.Skip("no POSIX shell (Git for Windows) installed")
		}
	}
	tm := newTestTerminalManager(t, t.TempDir())
	session, err := tm.startPipeSession("pipe-interactive", shell, nil)
	if err != nil {
		t.Fatalf("startPipeSession: %v", err)
	}
	tm.mutex.Lock()
	tm.sessions[session.ID] = session
	tm.mutex.Unlock()
	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// xterm.js sends CR for Enter; the output must contain the echoed line
	// and the command's own output.
	if err := tm.WriteRawInput(session.ID, "echo pipe-ok\r"); err != nil {
		t.Fatalf("WriteRawInput: %v", err)
	}
	var got strings.Builder
	deadline := time.After(10 * time.Second)
	for strings.Count(got.String(), "pipe-ok") < 2 {
		select {
		case chunk, ok := <-sub.ch:
			if !ok {
				t.Fatalf("session closed; output so far %q", got.String())
			}
			got.Write(chunk)
		case <-deadline:
			t.Fatalf("command did not run; output %q", got.String())
		}
	}
}

func TestTranslateNewlines(t *testing.T) {
	cases := []struct {
		in, want       string
		prevCR, endsCR bool
	}{
		{"a\nb\n", "a\r\nb\r\n", false, false},
		{"a\r\nb", "a\r\nb", false, false},
		{"\nx", "\nx", true, false},
		{"tail\r", "tail\r", false, true},
		{"", "", false, false},
	}
	for _, tc := range cases {
		got, cr := translateNewlines([]byte(tc.in), tc.prevCR)
		if string(got) != tc.want || cr != tc.endsCR {
			t.Errorf("translateNewlines(%q, %v) = %q, %v; want %q, %v", tc.in, tc.prevCR, got, cr, tc.want, tc.endsCR)
		}
	}
}
