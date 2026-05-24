package console

import (
	"testing"
)

func TestBoldText_WrapsTextWithANSICodes(t *testing.T) {
	// Arrange
	input := "hello"
	expected := ColorBold + "hello" + ColorReset

	// Act
	result := BoldText(input)

	// Assert
	if result != expected {
		t.Errorf("BoldText(%q) = %q, want %q", input, result, expected)
	}
}

func TestBoldText_EmptyString(t *testing.T) {
	// Arrange
	input := ""
	expected := ColorBold + "" + ColorReset

	// Act
	result := BoldText(input)

	// Assert
	if result != expected {
		t.Errorf("BoldText(%q) = %q, want %q", input, result, expected)
	}
}

func TestBoldText_MultiWord(t *testing.T) {
	// Arrange
	input := "foo bar baz"
	expected := ColorBold + "foo bar baz" + ColorReset

	// Act
	result := BoldText(input)

	// Assert
	if result != expected {
		t.Errorf("BoldText(%q) = %q, want %q", input, result, expected)
	}
}

func TestStdoutIsTerminal_ReturnsFalseInTestEnvironment(t *testing.T) {
	// In a test environment (non-TTY), stdout is not a terminal
	result := StdoutIsTerminal()
	if result {
		t.Error("StdoutIsTerminal() = true, want false in test environment")
	}
}

func TestStderrIsTerminal_ReturnsFalseInTestEnvironment(t *testing.T) {
	// In a test environment (non-TTY), stderr is not a terminal
	result := StderrIsTerminal()
	if result {
		t.Error("StderrIsTerminal() = true, want false in test environment")
	}
}

func TestFormatYesNoPrompt_NoDefault_NonTTY_ReturnsPlain(t *testing.T) {
	// In a test environment, stderr is not a TTY, so plain text is returned
	result := FormatYesNoPrompt(false)
	expected := "[y/N]"
	if result != expected {
		t.Errorf("FormatYesNoPrompt(false) = %q, want %q", result, expected)
	}
}

func TestFormatYesNoPrompt_YesDefault_NonTTY_ReturnsPlain(t *testing.T) {
	// In a test environment, stderr is not a TTY, so plain text is returned
	result := FormatYesNoPrompt(true)
	expected := "[Y/n]"
	if result != expected {
		t.Errorf("FormatYesNoPrompt(true) = %q, want %q", result, expected)
	}
}

func TestFormatYesNoPromptStdout_NoDefault_NonTTY_ReturnsPlain(t *testing.T) {
	// In a test environment, stdout is not a TTY, so plain text is returned
	result := FormatYesNoPromptStdout(false)
	expected := "[y/N]"
	if result != expected {
		t.Errorf("FormatYesNoPromptStdout(false) = %q, want %q", result, expected)
	}
}

func TestFormatYesNoPromptStdout_YesDefault_NonTTY_ReturnsPlain(t *testing.T) {
	// In a test environment, stdout is not a TTY, so plain text is returned
	result := FormatYesNoPromptStdout(true)
	expected := "[Y/n]"
	if result != expected {
		t.Errorf("FormatYesNoPromptStdout(true) = %q, want %q", result, expected)
	}
}
