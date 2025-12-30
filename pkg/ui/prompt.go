package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultPrompt = "Enter option number (or 0 to cancel): "
)

// NumericPromptOption represents a single option in a numeric prompt
type NumericPromptOption struct {
	Index       int
	DisplayName string
	Description string
	Value       string
}

// PromptForSelection prompts the user to select from a numbered list of options
// Returns the 1-based index of the selected option, or 0 if cancelled, and ok=true if valid
func PromptForSelection(options []string, prompt string) (int, bool) {
	if prompt == "" {
		prompt = DefaultPrompt
	}

	fmt.Printf("\n%s ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return 0, false
	}

	input := strings.TrimSpace(scanner.Text())
	selection, err := strconv.Atoi(input)
	if err != nil {
		fmt.Println("Invalid input. Please enter a number.")
		return 0, false
	}

	// Check for cancellation
	if selection == 0 {
		fmt.Println("Cancelled.")
		return 0, true
	}

	// Validate selection is within range
	if selection < 1 || selection > len(options) {
		fmt.Printf("Invalid selection. Please enter a number between 1 and %d.\n", len(options))
		return 0, false
	}

	return selection, true
}

// PromptForConfirmation prompts the user for yes/no confirmation
// Supports y, yes, Y, YES (yes) and anything else (no)
func PromptForConfirmation(prompt string) bool {
	if prompt == "" {
		prompt = "Continue? (y/n): "
	}

	fmt.Printf("%s ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}

	input := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return strings.HasPrefix(input, "y")
}

// DisplayNumberedList displays a numbered list of options
func DisplayNumberedList(items []string) {
	for i, item := range items {
		fmt.Printf("%d. %s\n", i+1, item)
	}
}

// DisplayNumberedListWithDescriptions displays a numbered list with descriptions
func DisplayNumberedListWithDescriptions(options []NumericPromptOption) {
	for _, opt := range options {
		label := fmt.Sprintf("%d. %s", opt.Index, opt.DisplayName)
		if opt.Description != "" {
			label += fmt.Sprintf(" - %s", opt.Description)
		}
		fmt.Println(label)
	}
}

// PromptForSelectionWithOptions prompts user to select from typed options
func PromptForSelectionWithOptions(options []NumericPromptOption, prompt string) (int, bool) {
	if len(options) == 0 {
		fmt.Println("No options available.")
		return 0, false
	}

	DisplayNumberedListWithDescriptions(options)
	return PromptForSelection(nil, prompt)
}
