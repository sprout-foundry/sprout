package console

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RenderMessage represents a UI update request
type RenderMessage struct {
	Type      string      // "content", "footer", "input", "cursor"
	Component string      // Component name
	Data      interface{} // Type-specific data
	Priority  int         // Higher priority processes first
	Callback  chan error  // Optional callback for completion
}

// ContentUpdate represents streaming content to append
type ContentUpdate struct {
	Text string
	// Future: could add styling, position info, etc
}

// FooterUpdate represents footer state change
type FooterUpdate struct {
	Model         string
	Provider      string
	Tokens        int
	Cost          float64
	Iteration     int
	ContextTokens int
	MaxTokens     int
}

// UIRenderer manages serialized UI updates through a message queue
type UIRenderer struct {
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan RenderMessage
	tm      TerminalManager

	// Component registry
	components map[string]Component

	// Render state
	isRendering bool
	renderMutex sync.Mutex

	// Stats for debugging
	messagesProcessed uint64
	lastRenderTime    time.Time

	// Batching for efficiency
	batchMode     bool
	batchInterval time.Duration
	batchBuffer   []RenderMessage
}

// NewUIRenderer creates a new UI renderer
func NewUIRenderer(tm TerminalManager) *UIRenderer {
	ctx, cancel := context.WithCancel(context.Background())

	return &UIRenderer{
		ctx:           ctx,
		cancel:        cancel,
		msgChan:       make(chan RenderMessage, 100), // Buffered for performance
		tm:            tm,
		components:    make(map[string]Component),
		batchInterval: 16 * time.Millisecond, // ~60fps if batching enabled
	}
}

// Start begins the render loop
func (ur *UIRenderer) Start() {
	go ur.renderLoop()
}

// Stop shuts down the renderer
func (ur *UIRenderer) Stop() {
	ur.cancel()
}

// RegisterComponent registers a component for rendering
func (ur *UIRenderer) RegisterComponent(name string, component Component) {
	ur.mu.Lock()
	defer ur.mu.Unlock()
	ur.components[name] = component
}

// SendMessage queues a render message
func (ur *UIRenderer) SendMessage(msg RenderMessage) error {
	select {
	case ur.msgChan <- msg:
		return nil
	case <-ur.ctx.Done():
		return fmt.Errorf("renderer stopped")
	default:
		// Channel full, try with small timeout
		select {
		case ur.msgChan <- msg:
			return nil
		case <-time.After(100 * time.Millisecond):
			return fmt.Errorf("render queue full")
		}
	}
}

// SendMessageSync sends a message and waits for completion
func (ur *UIRenderer) SendMessageSync(msg RenderMessage) error {
	msg.Callback = make(chan error, 1)
	if err := ur.SendMessage(msg); err != nil {
		return err
	}

	select {
	case err := <-msg.Callback:
		return err
	case <-ur.ctx.Done():
		return fmt.Errorf("renderer stopped")
	}
}

// AppendContent is a convenience method for streaming content
func (ur *UIRenderer) AppendContent(text string) error {
	return ur.SendMessage(RenderMessage{
		Type:      "content",
		Component: "agent",
		Data: ContentUpdate{
			Text: text,
		},
		Priority: 10, // High priority for responsiveness
	})
}

// UpdateFooter is a convenience method for footer updates
func (ur *UIRenderer) UpdateFooter(update FooterUpdate) error {
	return ur.SendMessage(RenderMessage{
		Type:      "footer",
		Component: "footer",
		Data:      update,
		Priority:  5, // Lower priority than content
	})
}

// renderLoop is the main event loop
func (ur *UIRenderer) renderLoop() {
	ticker := time.NewTicker(ur.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ur.ctx.Done():
			return

		case msg := <-ur.msgChan:
			if ur.batchMode {
				// Add to batch buffer
				ur.batchBuffer = append(ur.batchBuffer, msg)

				// Check if we should flush
				if len(ur.batchBuffer) >= 10 || msg.Priority >= 100 {
					ur.processBatch()
				}
			} else {
				// Process immediately
				ur.processMessage(msg)
			}

		case <-ticker.C:
			// Batch timer - flush any pending messages
			if ur.batchMode && len(ur.batchBuffer) > 0 {
				ur.processBatch()
			}
		}
	}
}

// processBatch processes all batched messages
func (ur *UIRenderer) processBatch() {
	if len(ur.batchBuffer) == 0 {
		return
	}

	// Sort by priority (higher first)
	// In real implementation, would use sort.Slice

	// Group updates by type to minimize terminal operations
	contentUpdates := []ContentUpdate{}
	var footerUpdate *FooterUpdate

	for _, msg := range ur.batchBuffer {
		switch msg.Type {
		case "content":
			if update, ok := msg.Data.(ContentUpdate); ok {
				contentUpdates = append(contentUpdates, update)
			}
		case "footer":
			if update, ok := msg.Data.(FooterUpdate); ok {
				footerUpdate = &update // Last footer update wins
			}
		}

		// Send callback if present
		if msg.Callback != nil {
			msg.Callback <- nil
		}
	}

	// Apply batched updates efficiently
	ur.renderMutex.Lock()
	defer ur.renderMutex.Unlock()

	// Save cursor once
	ur.tm.SaveCursor()

	// Apply content updates
	if len(contentUpdates) > 0 {
		// Combine all content into single write
		var combined string
		for _, update := range contentUpdates {
			combined += update.Text
		}
		if _, ok := ur.components["agent"]; ok {
			// Direct write through component
			// In real implementation, component would handle this
			fmt.Print(combined)
		}
	}

	// Apply footer update if any
	if footerUpdate != nil {
		if component, ok := ur.components["footer"]; ok {
			// Update footer state and render
			// In real implementation, would call component methods
			component.Render()
		}
	}

	// Restore cursor once
	ur.tm.RestoreCursor()

	// Clear batch
	ur.batchBuffer = ur.batchBuffer[:0]
	ur.messagesProcessed += uint64(len(ur.batchBuffer))
}

// processMessage processes a single message
func (ur *UIRenderer) processMessage(msg RenderMessage) {
	ur.renderMutex.Lock()
	defer ur.renderMutex.Unlock()

	defer func() {
		if msg.Callback != nil {
			msg.Callback <- nil
		}
		ur.messagesProcessed++
		ur.lastRenderTime = time.Now()
	}()

	switch msg.Type {
	case "content":
		ur.processContentUpdate(msg)
	case "footer":
		ur.processFooterUpdate(msg)
	case "input":
		ur.processInputUpdate(msg)
	case "cursor":
		ur.processCursorUpdate(msg)
	default:
		// Unknown message type
	}
}

// processContentUpdate handles streaming content
func (ur *UIRenderer) processContentUpdate(msg RenderMessage) {
	if update, ok := msg.Data.(ContentUpdate); ok {
		// Save/restore cursor to prevent interference
		ur.tm.SaveCursor()
		defer ur.tm.RestoreCursor()

		// Write content through component
		if _, ok := ur.components[msg.Component]; ok {
			// In real implementation, would call component.AppendContent or similar
			fmt.Print(update.Text)
		}
	}
}

// processFooterUpdate handles footer state changes
func (ur *UIRenderer) processFooterUpdate(msg RenderMessage) {
	if _, ok := msg.Data.(FooterUpdate); ok {
		// Update footer through component
		if component, ok := ur.components[msg.Component]; ok {
			// In real implementation, would update footer state then render
			component.Render()
		}
	}
}

// processInputUpdate handles input field updates
func (ur *UIRenderer) processInputUpdate(msg RenderMessage) {
	// Implementation depends on input component interface
}

// processCursorUpdate handles cursor positioning
func (ur *UIRenderer) processCursorUpdate(msg RenderMessage) {
	// Implementation for cursor movement
}

// EnableBatching enables update batching for efficiency
func (ur *UIRenderer) EnableBatching(enabled bool) {
	ur.mu.Lock()
	defer ur.mu.Unlock()
	ur.batchMode = enabled
}

// SetBatchInterval sets the batch flush interval
func (ur *UIRenderer) SetBatchInterval(interval time.Duration) {
	ur.mu.Lock()
	defer ur.mu.Unlock()
	ur.batchInterval = interval
}
