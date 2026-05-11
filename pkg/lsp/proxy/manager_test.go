package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServerKey(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
		languageID    string
		wantContains  []string // parts that should be in the key
	}{
		{
			name:          "simple absolute path",
			workspacePath: "/foo/bar",
			languageID:    "go",
			wantContains:  []string{"/foo/bar", "go"},
		},
		{
			name:          "simple absolute path with typescript",
			workspacePath: "/workspace",
			languageID:    "typescript",
			wantContains:  []string{"/workspace", "typescript"},
		},
		{
			name:          "relative path (gets normalized to absolute)",
			workspacePath: "./src",
			languageID:    "go",
			wantContains:  []string{"go"}, // Can't predict exact absolute path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := serverKey(tt.workspacePath, tt.languageID)

			// Verify key is not empty
			assert.NotEmpty(t, key)

			// Verify key contains expected parts
			for _, part := range tt.wantContains {
				assert.Contains(t, key, part)
			}

			// Verify key has the separator
			assert.Contains(t, key, "|")
		})
	}
}

func TestServerKeyConsistency(t *testing.T) {
	t.Run("same inputs produce same key", func(t *testing.T) {
		key1 := serverKey("/workspace", "go")
		key2 := serverKey("/workspace", "go")

		assert.Equal(t, key1, key2)
	})

	t.Run("different language produces different key", func(t *testing.T) {
		key1 := serverKey("/workspace", "go")
		key2 := serverKey("/workspace", "typescript")

		assert.NotEqual(t, key1, key2)
	})

	t.Run("different workspace produces different key", func(t *testing.T) {
		key1 := serverKey("/workspace1", "go")
		key2 := serverKey("/workspace2", "go")

		assert.NotEqual(t, key1, key2)
	})
}

func TestNewManager(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("creates manager with zero count", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		assert.Equal(t, 0, m.Count())
	})

	t.Run("manager has default configs", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		configs := m.GetConfig()
		assert.NotEmpty(t, configs)
		assert.Greater(t, len(configs), 0)
	})

	t.Run("manager has Go and TypeScript configs", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		configs := m.GetConfig()

		goCfg := FindLanguageServerByID("go", configs)
		assert.NotNil(t, goCfg)

		tsCfg := FindLanguageServerByID("typescript", configs)
		assert.NotNil(t, tsCfg)
	})
}

func TestManagerGetOrCreateUnknownLanguage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager(ctx)
	defer m.Close()

	t.Run("returns error for unknown language", func(t *testing.T) {
		process, release, err := m.GetOrCreate("/tmp", "unknown-language-xyz")

		assert.Nil(t, process)
		assert.Nil(t, release)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown language")
	})

	t.Run("returns error for empty language ID", func(t *testing.T) {
		process, release, err := m.GetOrCreate("/tmp", "")

		assert.Nil(t, process)
		assert.Nil(t, release)
		assert.Error(t, err)
	})
}

func TestManagerEvictIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("evict on empty manager does nothing", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.EvictIdle(1 * time.Minute)

		assert.Equal(t, 0, m.Count())
	})

	t.Run("evict with zero timeout is safe", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Should not panic even with zero timeout
		m.EvictIdle(0)

		assert.Equal(t, 0, m.Count())
	})

	t.Run("evict with negative timeout is safe", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Should not panic even with negative timeout
		m.EvictIdle(-1 * time.Minute)

		assert.Equal(t, 0, m.Count())
	})

	t.Run("evict with large timeout is safe", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Should not panic even with very large timeout
		m.EvictIdle(1000 * time.Hour)

		assert.Equal(t, 0, m.Count())
	})
}

func TestManagerCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(ctx)

	t.Run("cleanup on empty manager", func(t *testing.T) {
		// Cleanup should not panic on empty manager
		m.Cleanup()

		// Wait for the cleanup goroutine to finish
		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 0, m.Count())
	})

	t.Run("close is an alias for cleanup", func(t *testing.T) {
		m2 := NewManager(context.Background())

		// Should not panic
		m2.Close()

		// Wait for the cleanup goroutine to finish
		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 0, m2.Count())
	})

	t.Run("cleanup multiple times is safe", func(t *testing.T) {
		m3 := NewManager(context.Background())

		// Cleanup multiple times - should not panic
		m3.Cleanup()
		m3.Cleanup()
		m3.Cleanup()

		// Wait for goroutine
		time.Sleep(100 * time.Millisecond)
	})

	_ = cancel // suppress warning
}

func TestManagerSetConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("set and get custom configs", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

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
		m := NewManager(ctx)
		defer m.Close()

		emptyConfigs := []LanguageServerConfig{}
		m.SetConfig(emptyConfigs)

		retrieved := m.GetConfig()
		assert.Empty(t, retrieved)
	})

	t.Run("set configs modifies internal state", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		initial := m.GetConfig()
		initialCount := len(initial)

		// Set new configs
		newConfigs := []LanguageServerConfig{
			{
				ID:          "test",
				LanguageIDs: []string{"test"},
				Binary:      "test",
				Args:        []string{},
			},
		}
		m.SetConfig(newConfigs)

		// Get should return new configs
		retrieved := m.GetConfig()
		assert.NotEqual(t, initialCount, len(retrieved))
		assert.Len(t, retrieved, 1)
		assert.Equal(t, "test", retrieved[0].ID)
	})

	t.Run("set configs multiple times", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

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

func TestManagerCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("count returns zero for new manager", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		assert.Equal(t, 0, m.Count())
	})

	t.Run("count is stable", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Multiple calls should return same value
		count1 := m.Count()
		count2 := m.Count()
		count3 := m.Count()

		assert.Equal(t, count1, count2)
		assert.Equal(t, count2, count3)
	})
}

func TestManagerGetConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("get config returns default configs", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		configs := m.GetConfig()

		// Should have at least Go and TypeScript
		assert.NotEmpty(t, configs)
		assert.Greater(t, len(configs), 0)

		// Check for expected language servers
		hasGo := false
		hasTypeScript := false
		for _, cfg := range configs {
			if cfg.ID == "go" {
				hasGo = true
			}
			if cfg.ID == "typescript" {
				hasTypeScript = true
			}
		}

		assert.True(t, hasGo, "default configs should include Go")
		assert.True(t, hasTypeScript, "default configs should include TypeScript")
	})

	t.Run("get config returns a copy or reference", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		configs1 := m.GetConfig()
		configs2 := m.GetConfig()

		// Should return the same slice reference or equal copies
		assert.Equal(t, configs1, configs2)
	})
}

func TestManagerContextCancellation(t *testing.T) {
	t.Run("cleanup goroutine exits on context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		m := NewManager(ctx)

		// Cancel the context
		cancel()

		// Close should wait for the cleanup goroutine
		m.Close()

		// If we get here, the goroutine exited cleanly
		// No assertion needed - not hanging is the test
	})

	t.Run("close after context cancel is safe", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		m := NewManager(ctx)

		cancel()
		time.Sleep(50 * time.Millisecond) // Let goroutine exit

		// Should not panic
		m.Close()
	})
}

func TestManagerWithCustomConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("custom config is used for lookup", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Set a config with cat binary (will succeed but not be a real LSP server)
		customConfig := []LanguageServerConfig{
			{
				ID:          "test",
				LanguageIDs: []string{"test"},
				Binary:      "cat",
				Args:        []string{},
			},
		}
		m.SetConfig(customConfig)

		// Try to get a process for the test language
		// This should succeed in finding the config but may fail to start
		// since cat isn't a real LSP server (but it exists on PATH)
		_, release, err := m.GetOrCreate("/tmp", "test")

		// Either succeeds (cat started) or fails (cat isn't a proper LSP)
		if err != nil {
			// Failed to start - that's OK for this test
			assert.Nil(t, release)
		} else {
			// Succeeded - cat started, now release it
			release()
		}
	})
}

func TestManagerServerKeyEdgeCases(t *testing.T) {
	t.Run("handles workspace with spaces", func(t *testing.T) {
		// Note: This will be normalized by filepath.Abs
		key := serverKey("/path with spaces", "go")
		assert.NotEmpty(t, key)
		assert.Contains(t, key, "go")
	})

	t.Run("handles special characters in workspace", func(t *testing.T) {
		key := serverKey("/path-with_special.chars", "typescript")
		assert.NotEmpty(t, key)
		assert.Contains(t, key, "typescript")
	})

	t.Run("handles empty language ID", func(t *testing.T) {
		key := serverKey("/workspace", "")
		assert.NotEmpty(t, key)
		// Should still contain the separator
		assert.Contains(t, key, "|")
	})
}
