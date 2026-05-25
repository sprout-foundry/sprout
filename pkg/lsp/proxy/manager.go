// Package proxy provides an LSP (Language Server Protocol) proxy manager that
// manages language server processes for workspace-aware code intelligence.
//
// Manager Shutdown Contract:
//
// The Manager must be cleaned up via its Cleanup() (or Close()) method when the server
// shuts down. Cleanup() cancels the internal context, waits for the cleanup loop goroutine
// to exit via sync.WaitGroup, and closes all managed LSP processes. The ReactWebServer
// calls lspManager.Cleanup() during shutdown in server_lifecycle.go.
package proxy

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// Manager manages LSP server processes across workspaces and languages.
type Manager struct {
	mu       sync.Mutex
	servers  map[string]*serverEntry // key: "workspacePath|languageID"
	configs  []LanguageServerConfig
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// serverEntry tracks an LSP process and its usage.
type serverEntry struct {
	process  *LSPProcess
	lastUsed time.Time
	refCount int
}

// NewManager creates a new LSP process manager.
// Call Cleanup() to stop it.
func NewManager(ctx context.Context) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		servers: make(map[string]*serverEntry),
		configs: DefaultLanguageServers(),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start background cleanup goroutine
	m.wg.Add(1)
	go m.cleanupLoop()

	return m
}

// serverKey creates a unique key for a workspace+language pair.
func serverKey(workspacePath, languageID string) string {
	// Normalize path to ensure consistent key
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		absPath = workspacePath
	}
	return fmt.Sprintf("%s|%s", absPath, languageID)
}

// GetOrCreate returns an existing LSP process for the workspace+language, or starts a new one.
// Returns the process and a release function that should be called when the connection is done.
func (m *Manager) GetOrCreate(workspacePath, languageID string) (*LSPProcess, func(), error) {
	key := serverKey(workspacePath, languageID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we already have a process
	if entry, ok := m.servers[key]; ok {
		if entry.process.Healthy() {
			entry.refCount++
			entry.lastUsed = time.Now()

			release := func() {
				m.mu.Lock()
				defer m.mu.Unlock()
				if e, ok := m.servers[key]; ok && e.refCount > 0 {
					e.refCount--
					e.lastUsed = time.Now()
				}
			}

			return entry.process, release, nil
		}

		// Process is unhealthy, clean it up
		log.Printf("LSP manager: process for %s is unhealthy, restarting", key)
		entry.process.Close()
		delete(m.servers, key)
	}

	// Find the language server config
	cfg := FindLanguageServer(languageID, m.configs)
	if cfg == nil {
		// Try finding by ID
		cfg = FindLanguageServerByID(languageID, m.configs)
	}
	if cfg == nil {
		return nil, nil, fmt.Errorf("unknown language: %s", languageID)
	}

	// Resolve the binary path
	binaryPath, err := ResolveBinaryPath(cfg.Binary)
	if err != nil {
		hint := cfg.InstallHint
		if hint != "" {
			return nil, nil, fmt.Errorf("language server %q not found on PATH. Install with: %s", cfg.Binary, hint)
		}
		return nil, nil, fmt.Errorf("failed to find %s: %w", cfg.Binary, err)
	}

	// Start the process
	log.Printf("LSP manager: starting %s for workspace=%s", cfg.ID, workspacePath)
	process, err := StartLSPProcess(m.ctx, workspacePath, binaryPath, cfg.Args)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start %s: %w", cfg.Binary, err)
	}

	// Store the new process
	m.servers[key] = &serverEntry{
		process:  process,
		lastUsed: time.Now(),
		refCount: 1,
	}

	release := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if e, ok := m.servers[key]; ok && e.refCount > 0 {
			e.refCount--
			e.lastUsed = time.Now()
		}
	}

	return process, release, nil
}

// GetConfig returns the configured language server configs.
func (m *Manager) GetConfig() []LanguageServerConfig {
	return m.configs
}

// SetConfig sets the language server configs.
func (m *Manager) SetConfig(configs []LanguageServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = configs
}

// Count returns the number of active LSP processes.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.servers)
}

// cleanupLoop periodically evicts idle processes.
func (m *Manager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.EvictIdle(10 * time.Minute)
		}
	}
}

// EvictIdle removes LSP processes that haven't been used recently.
// The timeout specifies how long a process must be idle to be evicted.
func (m *Manager) EvictIdle(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-timeout)
	for key, entry := range m.servers {
		// Only evict if no active connections
		if entry.refCount == 0 && entry.lastUsed.Before(cutoff) {
			log.Printf("LSP manager: evicting idle process %s", key)
			entry.process.Close()
			delete(m.servers, key)
		}
	}
}

// Cleanup shuts down all LSP processes and removes them.
func (m *Manager) Cleanup() {
	m.cancel()
	m.wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, entry := range m.servers {
		log.Printf("LSP manager: closing process %s", key)
		entry.process.Close()
	}
	m.servers = make(map[string]*serverEntry)
}

// Close is an alias for Cleanup for API compatibility.
func (m *Manager) Close() {
	m.Cleanup()
}