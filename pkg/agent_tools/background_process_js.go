//go:build js

package tools

import (
	"context"
	"errors"
	"time"
)

// BackgroundProcess is a stub type for WASM builds.
// The real implementation is in background_process.go (!js build).
type BackgroundProcess struct{}

// BackgroundProcessManager is a stub type for WASM builds.
// Background process management is not available in WASM.
type BackgroundProcessManager struct{}

// NewBackgroundProcessManager returns nil in WASM builds.
func NewBackgroundProcessManager() *BackgroundProcessManager {
	return nil
}

// Start always returns an error in WASM builds.
func (m *BackgroundProcessManager) Start(_ context.Context, _ string, _ string) (string, error) {
	return "", errors.New("background process management is not available in WASM")
}

// StartWithKind always returns an error in WASM builds.
func (m *BackgroundProcessManager) StartWithKind(_ context.Context, _ string, _ string, _ string) (string, error) {
	return "", errors.New("background process management is not available in WASM")
}

// AdoptProcess always returns an error in WASM builds.
func (m *BackgroundProcessManager) AdoptProcess(_ any, _ string, _ string, _ string, _ <-chan error) (string, error) {
	return "", errors.New("background process management is not available in WASM")
}

// CheckOutput always returns an error in WASM builds.
func (m *BackgroundProcessManager) CheckOutput(_ string) (string, string, error) {
	return "", "", errors.New("background process management is not available in WASM")
}

// Stop always returns an error in WASM builds.
func (m *BackgroundProcessManager) Stop(_ string, _ time.Duration) error {
	return errors.New("background process management is not available in WASM")
}

// IsActive always returns false in WASM builds.
func (m *BackgroundProcessManager) IsActive(_ string) bool {
	return false
}

// StopAll is a no-op in WASM builds.
func (m *BackgroundProcessManager) StopAll() {}

// Close is a no-op in WASM builds.
func (m *BackgroundProcessManager) Close() {}

// GetProcess always returns nil in WASM builds.
func (m *BackgroundProcessManager) GetProcess(_ string) (*BackgroundProcess, bool) {
	return nil, false
}

// GetPID returns 0 in WASM builds (no real process).
func (p *BackgroundProcess) GetPID() int {
	return 0
}

// GetOutputPath returns empty string in WASM builds.
func (p *BackgroundProcess) GetOutputPath() string {
	return ""
}

// GetExitCode returns -1 in WASM builds (no real process).
func (p *BackgroundProcess) GetExitCode() int {
	return -1
}

// Done returns a closed channel in WASM builds (no real process to wait for).
func (p *BackgroundProcess) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// bpmContextKey is the context key for BackgroundProcessManager.
type bpmContextKey struct{}

// WithBackgroundProcessManager returns the context unchanged in WASM builds.
func WithBackgroundProcessManager(ctx context.Context, bpm *BackgroundProcessManager) context.Context {
	return ctx
}

// BackgroundProcessManagerFromContext always returns nil in WASM builds.
func BackgroundProcessManagerFromContext(ctx context.Context) *BackgroundProcessManager {
	return nil
}
