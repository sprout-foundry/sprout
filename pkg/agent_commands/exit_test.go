package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
)

func captureExitCommandOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestExitCommandPrintsContinuationForExistingSession(t *testing.T) {
	oldExit := exitProcess
	defer func() { exitProcess = oldExit }()
	exitProcess = func(code int) {}

	chatAgent := &agent.Agent{}
	chatAgent.SetSessionID("existing-session")

	output := captureExitCommandOutput(t, func() {
		_ = (&ExitCommand{}).Execute(nil, chatAgent)
	})

	if !strings.Contains(output, "To Continue: `ledit agent --session-id existing-session`") {
		t.Fatalf("expected continuation command in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Or Resume Latest: `ledit agent --last-session`") {
		t.Fatalf("expected --last-session hint in output, got:\n%s", output)
	}
}

func TestExitCommandCreatesSessionIDWhenMissing(t *testing.T) {
	oldExit := exitProcess
	defer func() { exitProcess = oldExit }()
	exitProcess = func(code int) {}

	chatAgent := &agent.Agent{}

	output := captureExitCommandOutput(t, func() {
		_ = (&ExitCommand{}).Execute(nil, chatAgent)
	})

	if chatAgent.GetSessionID() == "" {
		t.Fatal("expected session ID to be created when missing")
	}
	if !strings.Contains(output, "To Continue: `ledit agent --session-id ") {
		t.Fatalf("expected continuation command in output, got:\n%s", output)
	}
}
