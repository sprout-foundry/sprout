package proxy

import (
	"context"
	"io"
	"log"
	"os/exec"
	"sync"
)

// LSPProcess represents a running language server process.
type LSPProcess struct {
	cmd *exec.Cmd

	stdinPipe  io.WriteCloser
	stdoutPipe io.Reader

	closeMu sync.Mutex // protects closed and err
	closed  bool
	err     error // exit error

	subMu       sync.RWMutex // protects subscribers
	subscribers map[chan string]struct{}
}

// StartLSPProcess starts a language server process with the given binary and args.
// The process is started in the given workspace directory.
func StartLSPProcess(ctx context.Context, workspacePath, binary string, args []string) (*LSPProcess, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workspacePath

	// Set up stdin/stdout pipes
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, err
	}

	// LSP servers typically use stderr for logging - we don't capture it
	// but can add a pipe if needed for debugging

	// Start the process
	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		stdoutPipe.Close()
		return nil, err
	}

	proc := &LSPProcess{
		cmd:         cmd,
		stdinPipe:   stdinPipe,
		stdoutPipe:  stdoutPipe,
		subscribers: make(map[chan string]struct{}),
	}

	// Start a goroutine to read from stdout and broadcast to subscribers
	go proc.readLoop()

	return proc, nil
}

// readLoop reads framed messages from stdout and broadcasts them to all subscribers.
func (p *LSPProcess) readLoop() {
	reader := NewMessageReader(p.stdoutPipe)

	for {
		msg, err := reader.Read()
		if err != nil {
			// Process exited or error — drain and close all subscriber
			// channels under subMu to prevent double-close with Close().
			p.closeMu.Lock()
			p.err = err
			p.closeMu.Unlock()

			p.closeAllSubscribers()
			return
		}

		// Broadcast to all subscribers
		p.subMu.RLock()
		for ch := range p.subscribers {
			select {
			case ch <- msg:
			default:
				log.Printf("LSP process: dropping message for slow subscriber")
			}
		}
		p.subMu.RUnlock()
	}
}

// closeAllSubscribers safely closes and removes all subscriber channels.
// Both readLoop (on process exit) and Close() call this — subMu ensures
// each channel is closed exactly once.
func (p *LSPProcess) closeAllSubscribers() {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	for ch := range p.subscribers {
		close(ch)
	}
	p.subscribers = make(map[chan string]struct{})
}

// Send sends a raw JSON-RPC string to the LSP process (with Content-Length framing).
func (p *LSPProcess) Send(msg string) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if p.closed {
		return p.err
	}

	if err := WriteMessage(p.stdinPipe, msg); err != nil {
		return err
	}
	return nil
}

// Subscribe registers a channel to receive messages from the LSP server.
// The caller must read from the channel; close it when done.
// Returns the channel and an unsubscribe function.
func (p *LSPProcess) Subscribe() (<-chan string, func(), error) {
	ch := make(chan string, 256) // Buffered to prevent blocking

	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if p.closed {
		close(ch)
		return ch, func() {}, p.err
	}

	p.subMu.Lock()
	p.subscribers[ch] = struct{}{}
	p.subMu.Unlock()

	unsubscribe := func() {
		p.subMu.Lock()
		defer p.subMu.Unlock()
		if _, ok := p.subscribers[ch]; !ok {
			return // already unsubscribed by readLoop error path or Close()
		}
		delete(p.subscribers, ch)
		close(ch)
	}

	return ch, unsubscribe, nil
}

// Healthy returns true if the process is still running.
func (p *LSPProcess) Healthy() bool {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if p.closed {
		return false
	}

	// Send signal 0 to check if process is alive without killing it.
	// If the process has exited, this will return an error.
	if p.cmd.Process == nil {
		return false
	}
	return p.cmd.Process.Signal(nil) == nil
}

// Wait blocks until the process exits and returns the error.
func (p *LSPProcess) Wait() error {
	err := p.cmd.Wait()
	return err
}

// Close kills the process and cleans up resources.
func (p *LSPProcess) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if p.closed {
		return p.err
	}

	p.closed = true

	// Close all subscribers via the shared helper to prevent
	// double-close races with readLoop.
	p.closeAllSubscribers()

	// Close stdin first (this signals the LSP to shut down gracefully)
	if p.stdinPipe != nil {
		p.stdinPipe.Close()
	}

	// Kill the process if it's still running
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}

	return p.cmd.Wait()
}

// Process returns the underlying exec.Cmd for access to process info.
func (p *LSPProcess) Process() *exec.Cmd {
	return p.cmd
}
