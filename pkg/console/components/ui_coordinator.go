package components

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

// UIUpdate represents a UI update request
type UIUpdate struct {
	Type      string      // "content", "footer", "redraw", "clear"
	Data      interface{} // Update-specific data
	Timestamp time.Time
	Priority  int // Higher priority processes first
}

// UICoordinator manages coordinated updates between streaming content and footer
type UICoordinator struct {
	mu               sync.Mutex
	lastFooterUpdate time.Time
	footerThrottle   time.Duration
	isStreaming      bool
	pendingFooter    *FooterUpdate
	footerComponent  *FooterComponent
	updateChan       chan UIUpdate
	stopChan         chan struct{}
	terminalManager  interface {
		IsRawMode() bool
	}
}

// NewUICoordinator creates a new UI coordinator
func NewUICoordinator(footer *FooterComponent, tm interface{ IsRawMode() bool }) *UICoordinator {
	return &UICoordinator{
		footerComponent: footer,
		footerThrottle:  100 * time.Millisecond, // Update footer at most 10x per second
		updateChan:      make(chan UIUpdate, 100),
		stopChan:        make(chan struct{}),
		terminalManager: tm,
	}
}

// Start begins processing updates
func (uc *UICoordinator) Start() {
	go uc.processUpdates()
}

// Stop halts update processing
func (uc *UICoordinator) Stop() {
	close(uc.stopChan)
}

// SetStreaming marks streaming state
func (uc *UICoordinator) SetStreaming(streaming bool) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.isStreaming = streaming
}

// QueueFooterUpdate queues a footer update
func (uc *UICoordinator) QueueFooterUpdate(update FooterUpdate) {
	select {
	case uc.updateChan <- UIUpdate{
		Type:      "footer",
		Data:      update,
		Timestamp: time.Now(),
		Priority:  1, // Low priority
	}:
	default:
		// Channel full, drop update (footer updates are lossy)
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Dropped footer update due to full queue\n")
		}
	}
}

// QueueContentUpdate queues content to be written
func (uc *UICoordinator) QueueContentUpdate(content string) {
	select {
	case uc.updateChan <- UIUpdate{
		Type:      "content",
		Data:      content,
		Timestamp: time.Now(),
		Priority:  10, // High priority
	}:
	default:
		// Content is critical, block if needed
		uc.updateChan <- UIUpdate{
			Type:      "content",
			Data:      content,
			Timestamp: time.Now(),
			Priority:  10,
		}
	}
}

// QueueRedraw queues a full buffer redraw request
func (uc *UICoordinator) QueueRedraw(bufferHeight int, callback func(int) error) {
	select {
	case uc.updateChan <- UIUpdate{
		Type: "redraw",
		Data: RedrawRequest{
			BufferHeight: bufferHeight,
			Callback:     callback,
		},
		Timestamp: time.Now(),
		Priority:  5, // Medium priority - after content, before footer
	}:
	default:
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Dropped redraw request due to full queue\n")
		}
	}
}

// RedrawRequest contains information for buffer redraw
type RedrawRequest struct {
	BufferHeight int
	Callback     func(int) error
}

// processUpdates is the main update loop
func (uc *UICoordinator) processUpdates() {
	ticker := time.NewTicker(50 * time.Millisecond) // Check for pending footer updates
	defer ticker.Stop()

	for {
		select {
		case <-uc.stopChan:
			return

		case update := <-uc.updateChan:
			switch update.Type {
			case "content":
				// Content updates are always processed immediately
				// No need to do anything special - content is written directly

			case "footer":
				// Footer updates are throttled
				uc.handleFooterUpdate(update)

			case "redraw":
				// Buffer redraw requests
				uc.handleRedrawRequest(update)
			}

		case <-ticker.C:
			// Process any pending footer update
			uc.processPendingFooter()
		}
	}
}

// handleFooterUpdate handles a footer update request
func (uc *UICoordinator) handleFooterUpdate(update UIUpdate) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	footerUpdate, ok := update.Data.(FooterUpdate)
	if !ok {
		return
	}

	// Store as pending
	uc.pendingFooter = &footerUpdate

	// If not streaming or enough time has passed, update immediately
	if !uc.isStreaming || time.Since(uc.lastFooterUpdate) >= uc.footerThrottle {
		uc.applyFooterUpdateLocked()
	}
}

// processPendingFooter applies any pending footer update if throttle time has passed
func (uc *UICoordinator) processPendingFooter() {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	if uc.pendingFooter != nil && time.Since(uc.lastFooterUpdate) >= uc.footerThrottle {
		uc.applyFooterUpdateLocked()
	}
}

// applyFooterUpdateLocked applies the pending footer update (must hold lock)
func (uc *UICoordinator) applyFooterUpdateLocked() {
	if uc.pendingFooter == nil {
		return
	}

	// Update footer state
	uc.footerComponent.UpdateStats(
		uc.pendingFooter.Model,
		uc.pendingFooter.Provider,
		uc.pendingFooter.Tokens,
		uc.pendingFooter.Cost,
		uc.pendingFooter.Iteration,
		uc.pendingFooter.ContextTokens,
		uc.pendingFooter.MaxContextTokens,
	)

	// Save current terminal state before footer render
	savedRawMode := false
	if uc.terminalManager != nil && uc.terminalManager.IsRawMode() {
		savedRawMode = true
	}

	// CRITICAL: During streaming, we need extra care to prevent footer artifacts
	// from bleeding into the content area
	if uc.isStreaming {
		// First, ensure any pending content is flushed
		os.Stdout.Sync()

		// Save current cursor position AND scroll region state
		os.Stdout.Write([]byte("\033[s")) // Save cursor

		// Temporarily move cursor well outside any scroll region
		// Use a high line number that's definitely in the footer area
		os.Stdout.Write([]byte("\033[999;1H"))

		// Add a small delay to ensure terminal processes the move
		time.Sleep(1 * time.Millisecond)
	}

	// Render footer with proper cursor save/restore
	if err := uc.footerComponent.Render(); err != nil {
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Footer render error: %v\n", err)
		}
	}

	// Restore cursor position if we saved it
	if uc.isStreaming {
		// Ensure footer rendering is complete
		os.Stdout.Sync()

		// Restore cursor to its original position
		os.Stdout.Write([]byte("\033[u"))

		// Force another sync to ensure cursor is restored
		os.Stdout.Sync()
	}

	// CRITICAL: Ensure complete style reset after footer render
	// This prevents style bleed-through to streaming content
	resetSeq := "\033[0m" // Full reset
	if savedRawMode {
		// In raw mode, we need \r\n instead of just \n
		// But for style reset, we don't need line ending conversion
	}
	os.Stdout.Write([]byte(resetSeq))
	os.Stdout.Sync() // Force flush to ensure reset is applied

	uc.lastFooterUpdate = time.Now()
	uc.pendingFooter = nil
}

// handleRedrawRequest handles buffer redraw requests
func (uc *UICoordinator) handleRedrawRequest(update UIUpdate) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	// Don't redraw during active streaming
	if uc.isStreaming {
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Skipping redraw during streaming\n")
		}
		return
	}

	if req, ok := update.Data.(RedrawRequest); ok {
		// Execute the redraw callback
		if req.Callback != nil {
			if console.DebugEnabled() {
				fmt.Fprintf(os.Stderr, "[DEBUG] Executing buffer redraw\n")
			}

			// Ensure we reset styles before and after redraw
			os.Stdout.Write([]byte("\033[0m"))

			if err := req.Callback(req.BufferHeight); err != nil {
				if console.DebugEnabled() {
					fmt.Fprintf(os.Stderr, "[DEBUG] Redraw error: %v\n", err)
				}
			}

			// Reset styles again after redraw
			os.Stdout.Write([]byte("\033[0m\033[39;49m"))
			os.Stdout.Sync()
		}
	}
}

// FooterUpdate contains footer state to update
type FooterUpdate struct {
	Model            string
	Provider         string
	Tokens           int
	Cost             float64
	Iteration        int
	ContextTokens    int
	MaxContextTokens int
}
