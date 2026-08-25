package computer_use

// Rect represents a rectangular region on screen.
type Rect struct {
	X, Y, Width, Height int
}

// Size represents dimensions.
type Size struct {
	Width, Height int
}

// Point represents a coordinate.
type Point struct {
	X, Y int
}

// MouseButton specifies which mouse button.
type MouseButton string

const (
	MouseLeft   MouseButton = "left"
	MouseRight  MouseButton = "right"
	MouseMiddle MouseButton = "middle"
)

// ScrollDir specifies scroll direction.
type ScrollDir string

const (
	ScrollUp    ScrollDir = "up"
	ScrollDown  ScrollDir = "down"
	ScrollLeft  ScrollDir = "left"
	ScrollRight ScrollDir = "right"
)

// ComputerBackend is the platform-specific interface for desktop control.
// Phase 2 (SP-063-2) will implement this for macOS, Linux, Windows.
type ComputerBackend interface {
	Screenshot(region *Rect) (image []byte, dims Size, err error)
	MouseClick(x, y int, button MouseButton, double bool) error
	MouseDrag(from, to Point, button MouseButton) error
	MoveTo(x, y int) error
	KeyboardType(text string) error
	KeyboardPress(key string) error
	Scroll(dir ScrollDir, amount int, at *Point) error
}

// ---------------------------------------------------------------------------
// Package-level backend
// ---------------------------------------------------------------------------

var backend ComputerBackend = &MockBackend{}

// SetBackend sets the active backend for all tool handlers.
func SetBackend(b ComputerBackend) {
	backend = b
}

// GetBackend returns the current backend.
func GetBackend() ComputerBackend {
	return backend
}
