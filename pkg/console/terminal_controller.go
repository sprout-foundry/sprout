package console

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "sync"
    "time"
)

// RenderOp represents a terminal rendering operation
type RenderOp struct {
	Type     string
	Callback func() error
	Priority int // Higher priority ops execute first
}

// TerminalController centralizes all terminal operations and state management
type TerminalController struct {
	mu       sync.RWMutex
	tm       TerminalManager
	eventBus EventBus
	ctx      context.Context
	cancel   context.CancelFunc

	// Terminal state
	width     int
	height    int
	inRawMode bool

	// Resize handling
	resizeDebounce time.Duration
	lastResize     time.Time
	resizeChan     chan os.Signal

	// Interrupt handling
	interruptChan chan os.Signal
	onInterrupt   func()

	// Render queue for atomic updates
	renderQueue chan RenderOp
	renderWg    sync.WaitGroup

	// Component registry
	components     map[string]Component
	componentOrder []string
}

// NewTerminalController creates a new centralized terminal controller
func NewTerminalController(tm TerminalManager, eventBus EventBus) *TerminalController {
	ctx, cancel := context.WithCancel(context.Background())

	tc := &TerminalController{
		tm:             tm,
		eventBus:       eventBus,
		ctx:            ctx,
		cancel:         cancel,
		resizeDebounce: 100 * time.Millisecond, // Reduced from 200ms for better responsiveness
		renderQueue:    make(chan RenderOp, 100),
		components:     make(map[string]Component),
		componentOrder: []string{},
	}

	return tc
}

// Init initializes the terminal controller
func (tc *TerminalController) Init() error {
	// Get initial terminal size
	if err := tc.updateSize(); err != nil {
		return fmt.Errorf("failed to get initial terminal size: %w", err)
	}

	// Set up resize handling
    tc.resizeChan = make(chan os.Signal, 1)
    if sig := resizeSignal(); sig != nil {
        signal.Notify(tc.resizeChan, sig)
    } else {
        // No resize signal on this platform; leave channel unused (nil select-safe)
        tc.resizeChan = nil
    }

	// Set up interrupt handling
    tc.interruptChan = make(chan os.Signal, 1)
    intr := append([]os.Signal{os.Interrupt}, extraInterruptSignals()...)
    signal.Notify(tc.interruptChan, intr...)

	// Start event monitoring
	go tc.monitorEvents()

	// Start render queue processor
	tc.renderWg.Add(1)
	go tc.processRenderQueue()

	return nil
}

// Cleanup shuts down the controller and restores terminal state
func (tc *TerminalController) Cleanup() error {
	// Cancel context to stop all goroutines
	tc.cancel()

	// Close render queue and wait for processing to complete
	close(tc.renderQueue)
	tc.renderWg.Wait()

	// Stop signal handling
	if tc.resizeChan != nil {
		signal.Stop(tc.resizeChan)
		close(tc.resizeChan)
	}
	if tc.interruptChan != nil {
		signal.Stop(tc.interruptChan)
		close(tc.interruptChan)
	}

	// Restore terminal state
	return tc.tm.Cleanup()
}

// GetSize returns current terminal dimensions
func (tc *TerminalController) GetSize() (width, height int, err error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.width, tc.height, nil
}

// SetRawMode enables or disables raw mode atomically
func (tc *TerminalController) SetRawMode(enabled bool) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.inRawMode == enabled {
		return nil
	}

	if err := tc.tm.SetRawMode(enabled); err != nil {
		return err
	}

	tc.inRawMode = enabled
	return nil
}

// IsRawMode returns current raw mode state
func (tc *TerminalController) IsRawMode() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.inRawMode
}

// RegisterComponent adds a component to the controller
func (tc *TerminalController) RegisterComponent(name string, component Component, order int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.components[name] = component

	// Insert component name in order
	inserted := false
	for i := 0; i < len(tc.componentOrder); i++ {
		if order < i {
			tc.componentOrder = append(tc.componentOrder[:i], append([]string{name}, tc.componentOrder[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		tc.componentOrder = append(tc.componentOrder, name)
	}
}

// SetInterruptHandler sets the interrupt callback
func (tc *TerminalController) SetInterruptHandler(handler func()) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.onInterrupt = handler
}

// QueueRender adds a rendering operation to the queue
func (tc *TerminalController) QueueRender(op RenderOp) {
	select {
	case tc.renderQueue <- op:
	case <-tc.ctx.Done():
	}
}

// Write implements io.Writer with raw mode handling
func (tc *TerminalController) Write(p []byte) (n int, err error) {
	// Direct write for immediate output
	n, err = tc.tm.Write(p)
	// Flush immediately to ensure output is visible
	tc.tm.Flush()
	return n, err
}

// WriteString writes a string with proper line ending handling
func (tc *TerminalController) WriteString(s string) error {
	tc.QueueRender(RenderOp{
		Type: "writeString",
		Callback: func() error {
			_, err := tc.tm.WriteText(s)
			return err
		},
		Priority: 0,
	})
	return nil
}

// MoveCursor moves cursor immediately (required for proper rendering)
func (tc *TerminalController) MoveCursor(x, y int) error {
	// Cursor movement must be immediate for components like footer
	return tc.tm.MoveCursor(x, y)
}

// SetScrollRegion sets scroll region immediately (critical for layout)
func (tc *TerminalController) SetScrollRegion(top, bottom int) error {
	// Scroll region must be set immediately for proper layout
	return tc.tm.SetScrollRegion(top, bottom)
}

// ClearLine clears current line immediately
func (tc *TerminalController) ClearLine() error {
	// Line clearing must be immediate for proper rendering
	return tc.tm.ClearLine()
}

// Flush ensures all pending operations are rendered
func (tc *TerminalController) Flush() error {
	// Send a high-priority flush operation
	done := make(chan error, 1)
	tc.QueueRender(RenderOp{
		Type: "flush",
		Callback: func() error {
			err := tc.tm.Flush()
			done <- err
			return err
		},
		Priority: 999, // Highest priority
	})

	// Wait for flush to complete
	select {
	case err := <-done:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// Terminal returns the underlying terminal manager for compatibility
func (tc *TerminalController) Terminal() TerminalManager {
	return tc.tm
}

// ClearScreen clears the entire screen
func (tc *TerminalController) ClearScreen() error {
	tc.QueueRender(RenderOp{
		Type: "clearScreen",
		Callback: func() error {
			return tc.tm.ClearScreen()
		},
		Priority: 2,
	})
	return nil
}

// ClearToEndOfLine clears from cursor to end of line (immediate)
func (tc *TerminalController) ClearToEndOfLine() error {
	// Clear operations should be immediate for proper rendering
	return tc.tm.ClearToEndOfLine()
}

// ClearToEndOfScreen clears from cursor to end of screen
func (tc *TerminalController) ClearToEndOfScreen() error {
	tc.QueueRender(RenderOp{
		Type: "clearToEndOfScreen",
		Callback: func() error {
			return tc.tm.ClearToEndOfScreen()
		},
		Priority: 1,
	})
	return nil
}

// SaveCursor saves the current cursor position (immediate)
func (tc *TerminalController) SaveCursor() error {
	// Cursor save/restore must be immediate for proper operation
	return tc.tm.SaveCursor()
}

// RestoreCursor restores the saved cursor position (immediate)
func (tc *TerminalController) RestoreCursor() error {
	// Cursor save/restore must be immediate for proper operation
	return tc.tm.RestoreCursor()
}

// HideCursor hides the cursor (immediate)
func (tc *TerminalController) HideCursor() error {
	// Cursor visibility should be immediate
	return tc.tm.HideCursor()
}

// ShowCursor shows the cursor (immediate)
func (tc *TerminalController) ShowCursor() error {
	// Cursor visibility should be immediate
	return tc.tm.ShowCursor()
}

// WriteAt writes data at a specific position
func (tc *TerminalController) WriteAt(x, y int, data []byte) error {
	tc.QueueRender(RenderOp{
		Type: "writeAt",
		Callback: func() error {
			return tc.tm.WriteAt(x, y, data)
		},
		Priority: 0,
	})
	return nil
}

// WriteText writes text with automatic raw mode line ending handling
func (tc *TerminalController) WriteText(text string) (int, error) {
	// Direct write for immediate output
	n, err := tc.tm.WriteText(text)
	// Flush immediately to ensure output is visible
	tc.tm.Flush()
	return n, err
}

// ResetScrollRegion resets the scrolling region to the entire screen
func (tc *TerminalController) ResetScrollRegion() error {
	tc.QueueRender(RenderOp{
		Type: "resetScrollRegion",
		Callback: func() error {
			return tc.tm.ResetScrollRegion()
		},
		Priority: 2,
	})
	return nil
}

// ScrollUp scrolls the current region up by n lines
func (tc *TerminalController) ScrollUp(lines int) error {
	tc.QueueRender(RenderOp{
		Type: "scrollUp",
		Callback: func() error {
			return tc.tm.ScrollUp(lines)
		},
		Priority: 1,
	})
	return nil
}

// ScrollDown scrolls the current region down by n lines
func (tc *TerminalController) ScrollDown(lines int) error {
	tc.QueueRender(RenderOp{
		Type: "scrollDown",
		Callback: func() error {
			return tc.tm.ScrollDown(lines)
		},
		Priority: 1,
	})
	return nil
}

// OnResize registers a callback for terminal resize events
func (tc *TerminalController) OnResize(callback func(width, height int)) {
	// Subscribe to resize events
	tc.eventBus.Subscribe("terminal.resized", func(event Event) error {
		if data, ok := event.Data.(map[string]interface{}); ok {
			if w, wOk := data["width"].(int); wOk {
				if h, hOk := data["height"].(int); hOk {
					callback(w, h)
				}
			}
		}
		return nil
	})
}

// Private methods

func (tc *TerminalController) updateSize() error {
	width, height, err := tc.tm.GetSize()
	if err != nil {
		return err
	}

	tc.mu.Lock()
	tc.width = width
	tc.height = height
	tc.mu.Unlock()

	return nil
}

func (tc *TerminalController) monitorEvents() {
	for {
		select {
		case <-tc.ctx.Done():
			return

		case <-tc.resizeChan:
			tc.handleResize()

		case <-tc.interruptChan:
			tc.handleInterrupt()
		}
	}
}

func (tc *TerminalController) handleResize() {
	// Debounce rapid resize events
	now := time.Now()
	if now.Sub(tc.lastResize) < tc.resizeDebounce {
		return
	}
	tc.lastResize = now

	// Get new size
	oldWidth, oldHeight, _ := tc.GetSize()
	if err := tc.updateSize(); err != nil {
		return
	}

	newWidth, newHeight, _ := tc.GetSize()
	if newWidth == oldWidth && newHeight == oldHeight {
		return // No actual size change
	}

	// Publish resize event
	tc.eventBus.PublishAsync(Event{
		Type: "terminal.resized",
		Data: map[string]interface{}{
			"width":     newWidth,
			"height":    newHeight,
			"oldWidth":  oldWidth,
			"oldHeight": oldHeight,
		},
	})
}

func (tc *TerminalController) handleInterrupt() {
	tc.mu.RLock()
	handler := tc.onInterrupt
	tc.mu.RUnlock()

	if handler != nil {
		// Run handler in goroutine to avoid blocking
		go handler()
	}

	// Publish interrupt event
	tc.eventBus.PublishAsync(Event{
		Type: "terminal.interrupted",
		Data: map[string]interface{}{
			"time": time.Now(),
		},
	})
}

func (tc *TerminalController) processRenderQueue() {
	defer tc.renderWg.Done()

	// Batch operations for efficiency
	const batchTimeout = 5 * time.Millisecond
	batch := make([]RenderOp, 0, 10)
	timer := time.NewTimer(batchTimeout)
	timer.Stop()

	for {
		select {
		case op, ok := <-tc.renderQueue:
			if !ok {
				// Channel closed, process remaining batch
				tc.processBatch(batch)
				return
			}

			// Add to batch
			batch = append(batch, op)

			// Start timer if this is first op in batch
			if len(batch) == 1 {
				timer.Reset(batchTimeout)
			}

			// Process immediately if batch is full
			if len(batch) >= 10 {
				timer.Stop()
				tc.processBatch(batch)
				batch = batch[:0]
			}

		case <-timer.C:
			// Timeout reached, process batch
			if len(batch) > 0 {
				tc.processBatch(batch)
				batch = batch[:0]
			}

		case <-tc.ctx.Done():
			// Process remaining batch before exiting
			if len(batch) > 0 {
				tc.processBatch(batch)
			}
			return
		}
	}
}

func (tc *TerminalController) processBatch(batch []RenderOp) {
	if len(batch) == 0 {
		return
	}

	// Sort by priority (higher priority first)
	for i := 0; i < len(batch)-1; i++ {
		for j := i + 1; j < len(batch); j++ {
			if batch[j].Priority > batch[i].Priority {
				batch[i], batch[j] = batch[j], batch[i]
			}
		}
	}

	// Execute operations
	for _, op := range batch {
		if err := op.Callback(); err != nil {
			// Log error but continue processing
			fmt.Fprintf(os.Stderr, "Render error in %s: %v\n", op.Type, err)
		}
	}

	// Always flush after batch
	tc.tm.Flush()
}
