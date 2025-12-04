package console

// TerminalManager defines the interface for terminal operations
// used by the UI rendering system.
type TerminalManager interface {
	// SaveCursor saves the current cursor position
	SaveCursor() error
	
	// RestoreCursor restores the cursor to the previously saved position
	RestoreCursor() error
	
	// MoveCursor moves the cursor to the specified position (x, y)
	MoveCursor(x, y int) error
	
	// WriteText writes text at the current cursor position
	WriteText(text string) error
	
	// Flush ensures all output is written to the terminal
	Flush() error
	
	// GetSize returns the terminal dimensions
	GetSize() (width, height int, err error)
}