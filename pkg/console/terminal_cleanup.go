package console

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// TerminalCleanupHandler ensures terminal is properly restored on exit
type TerminalCleanupHandler struct {
	cleanupFuncs []func() error
	mu           sync.Mutex
	done         chan struct{}
}

// NewTerminalCleanupHandler creates a new cleanup handler
func NewTerminalCleanupHandler() *TerminalCleanupHandler {
	handler := &TerminalCleanupHandler{
		cleanupFuncs: make([]func() error, 0),
		done:         make(chan struct{}),
	}

	// Install signal handlers
	handler.installSignalHandlers()

	return handler
}

// Register adds a cleanup function to be called on exit
func (h *TerminalCleanupHandler) Register(fn func() error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupFuncs = append(h.cleanupFuncs, fn)
}

// Cleanup runs all registered cleanup functions
func (h *TerminalCleanupHandler) Cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Run cleanup functions in reverse order (LIFO)
	for i := len(h.cleanupFuncs) - 1; i >= 0; i-- {
		if err := h.cleanupFuncs[i](); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup error: %v\n", err)
		}
	}

	// Clear the list
	h.cleanupFuncs = h.cleanupFuncs[:0]
}

// installSignalHandlers sets up signal handlers for graceful shutdown
func (h *TerminalCleanupHandler) installSignalHandlers() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // Termination signal
		syscall.SIGQUIT, // Quit signal
		syscall.SIGHUP,  // Terminal hangup
	)

	go func() {
		sig := <-sigChan
		// Run cleanup on signal
		h.Cleanup()

		// Re-raise the signal to let the default handler run
		// This allows the application to exit naturally
		signal.Reset(sig)
		syscall.Kill(syscall.Getpid(), sig.(syscall.Signal))
	}()
}

// EnsureCleanup should be deferred in main to ensure cleanup on panic
func (h *TerminalCleanupHandler) EnsureCleanup() {
	if r := recover(); r != nil {
		// Cleanup on panic
		h.Cleanup()
		panic(r) // Re-panic after cleanup
	} else {
		// Normal cleanup
		h.Cleanup()
	}
}

// Global cleanup handler
var globalCleanupHandler = NewTerminalCleanupHandler()

// RegisterCleanup registers a global cleanup function
func RegisterCleanup(fn func() error) {
	globalCleanupHandler.Register(fn)
}

// RunCleanup runs all registered cleanup functions
func RunCleanup() {
	globalCleanupHandler.Cleanup()
}
