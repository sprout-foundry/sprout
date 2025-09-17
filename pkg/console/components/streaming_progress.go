package components

import (
	"fmt"
	"sync"
	"time"
)

// StreamingProgress shows a progress indicator during streaming
type StreamingProgress struct {
	mu        sync.Mutex
	active    bool
	stopChan  chan struct{}
	outputMux *sync.Mutex
}

// NewStreamingProgress creates a new progress indicator
func NewStreamingProgress(outputMux *sync.Mutex) *StreamingProgress {
	return &StreamingProgress{
		outputMux: outputMux,
		stopChan:  make(chan struct{}),
	}
}

// Start begins showing the progress indicator
func (sp *StreamingProgress) Start() {
	sp.mu.Lock()
	if sp.active {
		sp.mu.Unlock()
		return
	}
	sp.active = true
	sp.stopChan = make(chan struct{})
	sp.mu.Unlock()

	go sp.animate()
}

// Stop stops the progress indicator
func (sp *StreamingProgress) Stop() {
	sp.mu.Lock()
	if !sp.active {
		sp.mu.Unlock()
		return
	}
	sp.active = false
	close(sp.stopChan)
	sp.mu.Unlock()

	// Clear the progress line
	if sp.outputMux != nil {
		sp.outputMux.Lock()
		fmt.Print("\r\033[K")
		sp.outputMux.Unlock()
	}
}

// animate runs the progress animation
func (sp *StreamingProgress) animate() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frameIndex := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sp.stopChan:
			return
		case <-ticker.C:
			if sp.outputMux != nil {
				sp.outputMux.Lock()
				fmt.Printf("\r%s Streaming... ", frames[frameIndex])
				sp.outputMux.Unlock()
			}
			frameIndex = (frameIndex + 1) % len(frames)
		}
	}
}
