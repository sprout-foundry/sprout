package proxy

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestFindLanguageServer is in discovery_test.go
// TestFindLanguageServerByID is in discovery_test.go
// TestNormalizeLanguageID is in discovery_test.go

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

func TestManagerSetConfig(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	t.Run("set and get custom configs", func(t *testing.T) {
		customConfigs := []LanguageServerConfig{
			{
				ID:          "python",
				LanguageIDs: []string{"python"},
				Binary:      "pylsp",
				Args:        []string{"--stdio"},
			},
			{
				ID:          "rust",
				LanguageIDs: []string{"rust"},
				Binary:      "rls",
				Args:        []string{},
			},
		}

		m.SetConfig(customConfigs)

		retrieved := m.GetConfig()
		assert.Len(t, retrieved, 2)
		assert.Equal(t, "python", retrieved[0].ID)
		assert.Equal(t, "rust", retrieved[1].ID)
	})

	t.Run("set empty configs", func(t *testing.T) {
		emptyConfigs := []LanguageServerConfig{}
		m.SetConfig(emptyConfigs)

		retrieved := m.GetConfig()
		assert.Empty(t, retrieved)
	})

	t.Run("set configs modifies internal state", func(t *testing.T) {
		initial := m.GetConfig()
		initialCount := len(initial)

		// Set new configs
		newConfigs := []LanguageServerConfig{
			{
				ID:          "go",
				LanguageIDs: []string{"go"},
				Binary:      "gopls",
				Args:        []string{},
			},
		}
		m.SetConfig(newConfigs)

		// Get should return the new configs
		retrieved := m.GetConfig()
		assert.NotEqual(t, initialCount, len(retrieved))
		assert.Len(t, retrieved, 1)
	})

	t.Run("set configs multiple times", func(t *testing.T) {
		configs1 := []LanguageServerConfig{
			{ID: "test1", LanguageIDs: []string{"test1"}, Binary: "test1", Args: nil},
		}
		configs2 := []LanguageServerConfig{
			{ID: "test2", LanguageIDs: []string{"test2"}, Binary: "test2", Args: nil},
		}

		m.SetConfig(configs1)
		assert.Equal(t, "test1", m.GetConfig()[0].ID)

		m.SetConfig(configs2)
		assert.Equal(t, "test2", m.GetConfig()[0].ID)
	})
}

func TestManagerEvictIdleWithEntries(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	t.Run("evict idle entry with refCount=0", func(t *testing.T) {
		t.Parallel()

		// Start a real cat process
		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)
		defer realProc.Close()

		key := "/tmp|go-cat-1"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now().Add(-2 * time.Hour), // Very old
			refCount: 0,
		}
		m.mu.Unlock()

		// Verify it's there
		assert.Equal(t, 1, m.Count())

		// Evict entries older than 1 minute
		m.EvictIdle(1 * time.Minute)

		// Should be evicted
		assert.Equal(t, 0, m.Count())

		// Process should be closed
		assert.False(t, realProc.Healthy())
	})

	t.Run("do not evict entry with active connections", func(t *testing.T) {
		t.Parallel()

		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)
		defer realProc.Close()

		key := "/tmp|typescript-cat-2"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now().Add(-2 * time.Hour),
			refCount: 1, // Active connection
		}
		m.mu.Unlock()

		assert.Equal(t, 1, m.Count())

		// Evict entries older than 1 minute
		m.EvictIdle(1 * time.Minute)

		// Should NOT be evicted because refCount > 0
		assert.Equal(t, 1, m.Count())

		// Process should still be healthy
		assert.True(t, realProc.Healthy())
	})

	t.Run("do not evict recently used entry", func(t *testing.T) {
		t.Parallel()

		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)
		defer realProc.Close()

		key := "/tmp|javascript-cat-3"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now(), // Just now
			refCount: 0,
		}
		m.mu.Unlock()

		assert.Equal(t, 1, m.Count())

		// Evict entries older than 1 minute
		m.EvictIdle(1 * time.Minute)

		// Should NOT be evicted because it was used recently
		assert.Equal(t, 1, m.Count())

		// Process should still be healthy
		assert.True(t, realProc.Healthy())
	})

	t.Run("evict only idle entries", func(t *testing.T) {
		t.Parallel()

		// Add multiple entries with different states
		proc1, _ := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		proc2, _ := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		proc3, _ := StartLSPProcess(ctx, "/tmp", "cat", []string{})

		m.mu.Lock()
		m.servers["/tmp|old1"] = &serverEntry{
			process:  proc1,
			lastUsed: time.Now().Add(-2 * time.Hour),
			refCount: 0, // Should be evicted
		}
		m.servers["/tmp|old2"] = &serverEntry{
			process:  proc2,
			lastUsed: time.Now().Add(-2 * time.Hour),
			refCount: 1, // Should NOT be evicted (active connection)
		}
		m.servers["/tmp|new"] = &serverEntry{
			process:  proc3,
			lastUsed: time.Now(), // Just now
			refCount: 0, // Should NOT be evicted (recently used)
		}
		m.mu.Unlock()

		assert.Equal(t, 3, m.Count())

		// Evict entries older than 1 minute
		m.EvictIdle(1 * time.Minute)

		// Only entry2 and entry3 should remain
		assert.Equal(t, 2, m.Count())

		// Verify the right entries remain
		m.mu.Lock()
		_, exists1 := m.servers["/tmp|old1"]
		_, exists2 := m.servers["/tmp|old2"]
		_, exists3 := m.servers["/tmp|new"]
		m.mu.Unlock()

		assert.False(t, exists1, "old idle entry should be evicted")
		assert.True(t, exists2, "entry with active connection should remain")
		assert.True(t, exists3, "recently used entry should remain")

		// Clean up remaining processes
		proc2.Close()
		proc3.Close()
	})

	t.Run("process Close is called when evicted", func(t *testing.T) {
		t.Parallel()

		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)

		key := "/tmp|test-cat-4"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now().Add(-1 * time.Hour),
			refCount: 0,
		}
		m.mu.Unlock()

		// Process should be healthy
		assert.True(t, realProc.Healthy())

		// Evict the entry
		m.EvictIdle(1 * time.Minute)

		// Process should be closed
		assert.False(t, realProc.Healthy())
	})
}

func TestManagerGetOrCreateWithUnhealthyProcess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	t.Run("unhealthy process is cleaned up", func(t *testing.T) {
		t.Parallel()

		// We need to simulate an unhealthy process by starting one and closing it
		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)

		// Close the process to make it unhealthy
		realProc.Close()

		key := "/tmp|go-unhealthy-1"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now(),
			refCount: 0,
		}
		m.mu.Unlock()

		// Verify the entry exists
		assert.Equal(t, 1, m.Count())

		// Try to get or create
		// This should fail because gopls doesn't exist, but it should
		// clean up the unhealthy entry first
		_, _, err = m.GetOrCreate("/tmp", "go")

		// Should get an error because gopls doesn't exist
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to find")

		// The unhealthy entry should have been removed
		assert.Equal(t, 0, m.Count())
	})

	t.Run("healthy process would be reused (if binary existed)", func(t *testing.T) {
		t.Parallel()

		realProc, err := StartLSPProcess(ctx, "/tmp", "cat", []string{})
		require.NoError(t, err)
		defer realProc.Close()

		key := "/tmp|go-healthy-1"

		m.mu.Lock()
		m.servers[key] = &serverEntry{
			process:  realProc,
			lastUsed: time.Now(),
			refCount: 0,
		}
		m.mu.Unlock()

		// Verify the process is healthy
		assert.True(t, realProc.Healthy())

		// The key point is that the healthy check happens
		// We can't actually test GetOrCreate because gopls doesn't exist
		// But we've verified the unhealthy cleanup path

		// Clean up our test entry
		m.mu.Lock()
		delete(m.servers, key)
		m.mu.Unlock()
	})
}
