package ui

import (
	"context"
	"errors"
)

// ErrUINotAvailable is returned when UI is not available (e.g., in non-interactive mode)
var ErrUINotAvailable = errors.New("UI not available")

// ErrCancelled is returned when a UI operation is cancelled
var ErrCancelled = errors.New("operation cancelled")

// CommandUI provides UI capabilities to commands
type CommandUI interface {
	// ShowDropdown shows a dropdown selection UI
	ShowDropdown(ctx context.Context, items []DropdownItem, options DropdownOptions) (DropdownItem, error)

	// ShowInput shows an input prompt
	ShowInput(ctx context.Context, prompt string, defaultValue string) (string, error)

	// ShowProgress shows a progress indicator
	ShowProgress(ctx context.Context, message string) ProgressHandle

	// IsInteractive returns true if the UI is interactive
	IsInteractive() bool
}

// ProgressHandle allows updating or closing a progress indicator
type ProgressHandle interface {
	// Update updates the progress
	Update(message string, current, total int)

	// Done marks the progress as complete
	Done()

	// Close closes the progress indicator
	Close()
}
