package agent

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
)

// OutputBuffer captures agent output for controlled display
type OutputBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// NewOutputBuffer creates a new output buffer
func NewOutputBuffer() *OutputBuffer {
	return &OutputBuffer{}
}

// Printf captures formatted output
func (ob *OutputBuffer) Printf(format string, args ...interface{}) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	fmt.Fprintf(&ob.buffer, format, args...)
}

// Print captures output
func (ob *OutputBuffer) Print(args ...interface{}) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	fmt.Fprint(&ob.buffer, args...)
}

// Println captures output with newline
func (ob *OutputBuffer) Println(args ...interface{}) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	fmt.Fprintln(&ob.buffer, args...)
}

// GetOutput returns the captured output
func (ob *OutputBuffer) GetOutput() string {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	return ob.buffer.String()
}

// Clear clears the buffer
func (ob *OutputBuffer) Clear() {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.buffer.Reset()
}

// GetAndClear returns the output and clears the buffer
func (ob *OutputBuffer) GetAndClear() string {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	output := ob.buffer.String()
	ob.buffer.Reset()
	return strings.TrimSpace(output)
}
