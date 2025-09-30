package commands

import (
	"runtime"
	"strings"
	"testing"
)

func TestShellCommand_Name(t *testing.T) {
	cmd := &ShellCommand{}
	if cmd.Name() != "shell" {
		t.Errorf("Expected name 'shell', got %s", cmd.Name())
	}
}

func TestShellCommand_Description(t *testing.T) {
	cmd := &ShellCommand{}
	desc := cmd.Description()
	if !strings.Contains(desc, "shell scripts") {
		t.Errorf("Description should mention shell scripts, got: %s", desc)
	}
	if !strings.Contains(desc, "environmental context") {
		t.Errorf("Description should mention environmental context, got: %s", desc)
	}
}

func TestShellCommand_GatherEnvironmentalContext(t *testing.T) {
	cmd := &ShellCommand{}
	context, err := cmd.gatherEnvironmentalContext()

	if err != nil {
		t.Fatalf("gatherEnvironmentalContext failed: %v", err)
	}

	if context == "" {
		t.Fatal("Environmental context should not be empty")
	}

	// Check that context contains expected information
	expectedContent := []string{
		"Operating System:",
		"Architecture:",
		"Working Directory:",
		"Shell:",
		"Environment Variables:",
		"Available Tools:",
		runtime.GOOS,
		runtime.GOARCH,
	}

	for _, expected := range expectedContent {
		if !strings.Contains(context, expected) {
			t.Errorf("Environmental context missing '%s':\n%s", expected, context)
		}
	}

	// Check that PATH is included if it exists
	if strings.Contains(context, "PATH=") {
		if !strings.Contains(context, "PATH=") {
			t.Error("PATH environment variable should be included if it exists")
		}
	}
}

func TestShellCommand_Execute_NoArgs(t *testing.T) {
	cmd := &ShellCommand{}
	err := cmd.Execute([]string{}, nil)

	if err == nil {
		t.Error("Execute should return error when no args provided")
	}

	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Error should contain usage information, got: %v", err)
	}
}
