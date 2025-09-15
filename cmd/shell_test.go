package cmd

import (
	"strings"
	"testing"
)

func TestShellCommandArgsValidation(t *testing.T) {
	// Test that the command has proper argument validation configured
	if shellCmd.Args == nil {
		t.Error("Shell command should have argument validation configured")
	}

	// Test the direct validation - cobra.MinimumNArgs(1) should catch this
	validator := shellCmd.Args
	if validator != nil {
		err := validator(shellCmd, []string{})
		if err == nil {
			t.Error("Expected error when no arguments provided to validator, but got nil")
		}
	}

	// Test that validation passes with arguments
	if validator != nil {
		err := validator(shellCmd, []string{"test description"})
		if err != nil {
			t.Errorf("Expected no error with valid arguments, but got: %v", err)
		}
	}
}

func TestShellCommandHelp(t *testing.T) {
	// Test that the command is properly configured
	if shellCmd.Use != "shell [description]" {
		t.Errorf("Expected Use to be 'shell [description]', got %s", shellCmd.Use)
	}

	if shellCmd.Short == "" {
		t.Error("Short description should not be empty")
	}

	if shellCmd.Long == "" {
		t.Error("Long description should not be empty")
	}

	// Check that examples are included in the long description
	if !strings.Contains(shellCmd.Long, "Examples:") {
		t.Error("Long description should contain examples")
	}

	if !strings.Contains(shellCmd.Long, "ledit shell") {
		t.Error("Long description should contain usage examples")
	}
}