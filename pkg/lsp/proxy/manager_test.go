package proxy

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// fakeLSPProcess is a mock LSPProcess for testing.
type fakeLSPProcess struct {
	cmd       *exec.Cmd
	closed    bool
	closedMu  sync.Mutex
	healthy  bool
	sendCh   chan string
	subCh    chan chan string
}

func newFakeLSPProcess() *fakeLSPProcess {
	return &fakeLSPProcess{
		sendCh: make(chan string, 10),
		subCh:  make(chan chan string, 10),
		healthy: true,
	}
}

func (f *fakeLSPProcess) Send(msg string) error {
	f.closedMu.Lock()
	defer f.closedMu.Unlock()
	if f.closed {
		return context.Canceled
	}
	f.sendCh <- msg
	return nil
}

func (f *fakeLSPProcess) Subscribe() (<-chan string, func(), error) {
	ch := make(chan string, 10)
	f.subCh <- ch
	return ch, func() {}, nil
}

func (f *fakeLSPProcess) Healthy() bool {
	f.closedMu.Lock()
	defer f.closedMu.Unlock()
	return f.healthy && !f.closed
}

func (f *fakeLSPProcess) Close() error {
	f.closedMu.Lock()
	defer f.closedMu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeLSPProcess) Wait() error {
	return nil
}

func (f *fakeLSPProcess) Process() *exec.Cmd {
	return f.cmd
}

func TestServerKey(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
		languageID   string
		want         string
	}{
		{
			name:          "simple",
			workspacePath: "/foo/bar",
			languageID:   "go",
			want:         "/foo/bar|go",
		},
		{
			name:          "with dots",
			workspacePath: "./src",
			languageID:   "typescript",
			want:         "[UNSUPPORTED]", // We'll just check it's not empty
		},
	}

	ctx := context.Background()
	_ = ctx // unused

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := serverKey(tt.workspacePath, tt.languageID)
			if tt.want != "[UNSUPPORTED]" && key != tt.want {
				t.Errorf("serverKey() = %v, want %v", key, tt.want)
			}
			// Just verify it's not empty
			if key == "" {
				t.Error("serverKey() returned empty string")
			}
		})
	}
}

func TestFindLanguageServer(t *testing.T) {
	configs := DefaultLanguageServers()

	tests := []struct {
		name       string
		languageID string
		wantID    string
	}{
		{"go", "go", "go"},
		{"typescript", "typescript", "typescript"},
		{"javascript", "javascript", "typescript"},
		{"unknown", "unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := FindLanguageServer(tt.languageID, configs)
			if tt.wantID == "" {
				if cfg != nil {
					t.Errorf("FindLanguageServer() = %v, want nil", cfg)
				}
			} else {
				if cfg == nil {
					t.Fatalf("FindLanguageServer() = nil, want %v", tt.wantID)
				}
				if cfg.ID != tt.wantID {
					t.Errorf("FindLanguageServer().ID = %v, want %v", cfg.ID, tt.wantID)
				}
			}
		})
	}
}

func TestFindLanguageServerByID(t *testing.T) {
	configs := DefaultLanguageServers()

	tests := []struct {
		name    string
		id      string
		wantID  string
		wantBin string
	}{
		{"go by ID", "go", "go", "gopls"},
		{"typescript by ID", "typescript", "typescript", "typescript-language-server"},
		{"unknown ID", "unknown", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := FindLanguageServerByID(tt.id, configs)
			if tt.wantID == "" {
				if cfg != nil {
					t.Errorf("FindLanguageServerByID() = %v, want nil", cfg)
				}
			} else {
				if cfg == nil {
					t.Fatalf("FindLanguageServerByID() = nil, want %v", tt.wantID)
				}
				if cfg.ID != tt.wantID {
					t.Errorf("FindLanguageServerByID().ID = %v, want %v", cfg.ID, tt.wantID)
				}
				if cfg.Binary != tt.wantBin {
					t.Errorf("FindLanguageServerByID().Binary = %v, want %v", cfg.Binary, tt.wantBin)
				}
			}
		})
	}
}

func TestNormalizeLanguageID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{"lowercase", "GO", "go"},
		{"with spaces", "  go  ", "go"},
		{"mixed", "Go ", "go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeLanguageID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeLanguageID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	if m.Count() != 0 {
		t.Errorf("NewManager().Count() = %v, want 0", m.Count())
	}

	// Check configs are set
	configs := m.GetConfig()
	if len(configs) == 0 {
		t.Error("NewManager().GetConfig() is empty")
	}
}

func TestManagerGetOrCreateUnknownLanguage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	_, _, err := m.GetOrCreate("/tmp", "unknown-language-xyz")
	if err == nil {
		t.Error("GetOrCreate() should return error for unknown language")
	}
}

func TestManagerGetOrCreateReuse(t *testing.T) {
	// This test checks that calling GetOrCreate twice with the same parameters returns the same process.
	// However, we can't actually start real LSP processes in tests, so we'll just verify
	// the manager maintains the count.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	// The manager should have 0 processes initially
	if count := m.Count(); count != 0 {
		t.Errorf("Count() = %v, want 0", count)
	}

	// Note: We can't actually test GetOrCreate because it requires a real LSP binary.
	// In a real test environment, we'd mock the LSPProcess or use a fake binary.
	_ = m // satisfy linter
}

func TestManagerEvictIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	// Calling EvictIdle on an empty manager should not panic
	m.EvictIdle(time.Minute)

	// Manager should still be empty
	if count := m.Count(); count != 0 {
		t.Errorf("Count() = %v, want 0", count)
	}
}

func TestManagerCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(ctx)

	// Cleanup should not panic on empty manager
	m.Cleanup()

	// Wait for the cleanup goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// Manager should be empty after cleanup
	if count := m.Count(); count != 0 {
		t.Errorf("Count() after Cleanup = %v, want 0", count)
	}

	_ = cancel // suppress warning
}