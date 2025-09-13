package interactive

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// TerminalUI provides a terminal UI with a persistent footer
type TerminalUI struct {
	width       int
	height      int
	footerLines int
	stats       *UIStats
	mu          sync.Mutex
}

// UIStats holds the statistics to display in the footer
type UIStats struct {
	Model       string
	Provider    string
	TotalTokens int
	TotalCost   float64
	LastUpdated time.Time
}

// NewTerminalUI creates a new terminal UI
func NewTerminalUI() (*TerminalUI, error) {
	width, height, err := term.GetSize(0)
	if err != nil {
		// Default to 80x24 if we can't get terminal size
		width, height = 80, 24
	}

	return &TerminalUI{
		width:       width,
		height:      height,
		footerLines: 3, // Number of lines for the footer
		stats:       &UIStats{},
	}, nil
}

// UpdateStats updates the statistics displayed in the footer
func (ui *TerminalUI) UpdateStats(stats *UIStats) {
	ui.stats = stats
	ui.RedrawFooter()
}

// ClearFooter clears the footer area
func (ui *TerminalUI) ClearFooter() {
	// Move cursor to the footer area
	fmt.Printf("\033[%d;1H", ui.height-ui.footerLines+1)

	// Clear each footer line
	for i := 0; i < ui.footerLines; i++ {
		fmt.Print("\033[K") // Clear to end of line
		if i < ui.footerLines-1 {
			fmt.Print("\n")
		}
	}
}

// RedrawFooter redraws the footer with current stats
func (ui *TerminalUI) RedrawFooter() {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	// Save cursor position
	fmt.Print("\033[s")

	// Move to footer area
	fmt.Printf("\033[%d;1H", ui.height-ui.footerLines+1)

	// Draw separator line
	fmt.Print("\033[90m") // Gray color
	fmt.Println(strings.Repeat("─", ui.width))

	// Draw stats line
	statsLine := fmt.Sprintf(
		" 📡 %s (%s) | 🪙 Tokens: %d | 💰 Cost: $%.4f | 🕐 %s",
		ui.stats.Model,
		ui.stats.Provider,
		ui.stats.TotalTokens,
		ui.stats.TotalCost,
		ui.stats.LastUpdated.Format("15:04:05"),
	)

	// Truncate if too long
	if len(statsLine) > ui.width {
		statsLine = statsLine[:ui.width-3] + "..."
	}

	fmt.Print("\033[0m") // Reset color
	fmt.Println(statsLine)

	// Draw bottom border
	fmt.Print("\033[90m") // Gray color
	fmt.Print(strings.Repeat("─", ui.width))
	fmt.Print("\033[0m") // Reset color

	// Restore cursor position
	fmt.Print("\033[u")
}

// SetupScrollRegion sets up a scroll region above the footer
func (ui *TerminalUI) SetupScrollRegion() {
	// Set scroll region to exclude footer
	fmt.Printf("\033[1;%dr", ui.height-ui.footerLines)

	// Move cursor to top of scroll region
	fmt.Print("\033[1;1H")
}

// ResetScrollRegion resets the scroll region to full screen
func (ui *TerminalUI) ResetScrollRegion() {
	fmt.Printf("\033[1;%dr", ui.height)
}

// HandleResize updates the terminal dimensions
func (ui *TerminalUI) HandleResize() error {
	width, height, err := term.GetSize(0)
	if err != nil {
		return err
	}

	ui.width = width
	ui.height = height

	// Redraw everything
	ui.SetupScrollRegion()
	ui.RedrawFooter()

	return nil
}

// Clear clears the entire screen and redraws the UI
func (ui *TerminalUI) Clear() {
	// Clear screen
	fmt.Print("\033[2J")

	// Set up the UI
	ui.SetupScrollRegion()
	ui.RedrawFooter()

	// Move cursor to top
	fmt.Print("\033[1;1H")
}
