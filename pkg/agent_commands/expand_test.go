package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/cliui"
)

func runExpand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := &ExpandCommand{}
	cmd.SetOutput(&buf)
	err := cmd.Execute(args, nil)
	return buf.String(), err
}

func TestExpandCommand_PrintsMostRecentByDefault(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)
	cliui.RecordToolOutput("read_file", "a.go", "completed", "package a")
	cliui.RecordToolOutput("shell_command", "go test", "completed", "ok ./...")

	out, err := runExpand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "shell_command(go test)") {
		t.Fatalf("header missing: %q", out)
	}
	if !strings.Contains(out, "ok ./...") {
		t.Fatalf("body missing: %q", out)
	}
}

func TestExpandCommand_ByNumber(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)
	cliui.RecordToolOutput("read_file", "a.go", "completed", "package a")
	cliui.RecordToolOutput("shell_command", "go test", "completed", "ok ./...")

	out, err := runExpand(t, "1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "package a") {
		t.Fatalf("wrong entry: %q", out)
	}
	if strings.Contains(out, "ok ./...") {
		t.Fatalf("printed a second entry: %q", out)
	}
}

func TestExpandCommand_ListsOnRequest(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)
	cliui.RecordToolOutput("read_file", "a.go", "completed", "package a")

	out, err := runExpand(t, "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Recent tool output") || !strings.Contains(out, "read_file(a.go)") {
		t.Fatalf("list output = %q", out)
	}
}

func TestExpandCommand_UnknownNumberIsAnError(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)
	cliui.RecordToolOutput("read_file", "a.go", "completed", "package a")

	if _, err := runExpand(t, "42"); err == nil {
		t.Fatal("expected an error for an unknown number")
	}
}

func TestExpandCommand_EmptyRingExplainsItself(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)

	out, err := runExpand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No tool output buffered") {
		t.Fatalf("output = %q", out)
	}
}

func TestExpandCommand_ReportsEmptyResult(t *testing.T) {
	cliui.ResetToolOutputs()
	t.Cleanup(cliui.ResetToolOutputs)
	cliui.RecordToolOutput("grep", "needle", "completed", "")

	out, err := runExpand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "returned no output") {
		t.Fatalf("output = %q", out)
	}
}
