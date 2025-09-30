package ui

// DropdownItem represents an item that can be displayed in a dropdown
type DropdownItem interface {
	// Display returns the string to show in the dropdown
	Display() string
	// SearchText returns the text used for searching (can be same as Display)
	SearchText() string
	// Value returns the actual value when selected
	Value() interface{}
}

// DropdownOptions configures the dropdown behavior
type DropdownOptions struct {
	// Prompt shown above the items
	Prompt string
	// SearchPrompt shown at the bottom (legacy; kept for compatibility)
	SearchPrompt string
	// MaxHeight limits the number of items shown (0 = auto based on terminal)
	MaxHeight int
	// ShowCounts shows item counts in scroll indicators
	ShowCounts bool
}
