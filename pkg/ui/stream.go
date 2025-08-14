package ui

import (
	"bytes"
	"sync"
)

// StreamWriter is an io.Writer that forwards lines to the UI as LogEvents.
// It buffers partial lines between writes.
type StreamWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// NewStreamWriter creates a new StreamWriter.
func NewStreamWriter() *StreamWriter {
	// Notify UI that a stream has started
	PublishStreamStarted()
	return &StreamWriter{}
}

// Write implements io.Writer. It splits incoming bytes on newlines and
// publishes each complete line as a LogEvent, buffering any partial line.
func (w *StreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Append new data to buffer
	_, _ = w.buffer.Write(p)

	for {
		line, err := w.buffer.ReadString('\n')
		if err != nil {
			// No full line available; push bytes back by re-appending to buffer
			// We already consumed them, so put them back
			// Easiest: prepend line back by resetting and writing line
			// But ReadString only returns partial without delimiter when no '\n'
			// We reconstruct by writing the partial back to the front via a new buffer
			if len(line) > 0 {
				// Rebuild buffer with partial line at front
				remaining := w.buffer.Bytes()
				w.buffer.Reset()
				_, _ = w.buffer.WriteString(line)
				_, _ = w.buffer.Write(remaining)
			}
			break
		}
		// strip trailing '\n' in display; users still see progress as lines arrive
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		Log(line)
	}
	return len(p), nil
}

// Flush publishes any buffered partial line as a log event.
func (w *StreamWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buffer.Len() == 0 {
		PublishStreamEnded()
		return
	}
	Log(w.buffer.String())
	w.buffer.Reset()
	PublishStreamEnded()
}
