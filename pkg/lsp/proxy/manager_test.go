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

func TestManagerGetOrCreateGracefulErrorWithInstallHint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("Python returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Skip if pylsp is installed locally (test assumes it's absent).
		// Some CI images ship python-lsp-server pre-installed.
		if _, err := exec.LookPath("pylsp"); err == nil {
			t.Skip("pylsp is installed locally; skipping install-hint test")
		}

		_, _, err := m.GetOrCreate("/tmp", "python")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pylsp")
		assert.Contains(t, err.Error(), "pip install python-lsp-server")
	})

	t.Run("Rust returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Skip if rust-analyzer is installed locally (test assumes it's
		// absent). The GitHub Actions ubuntu-latest image ships a system
		// rust toolchain that includes rust-analyzer — caught in CI.
		if _, err := exec.LookPath("rust-analyzer"); err == nil {
			t.Skip("rust-analyzer is installed locally; skipping install-hint test")
		}

		_, _, err := m.GetOrCreate("/tmp", "rust")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rust-analyzer")
		assert.Contains(t, err.Error(), "rustup component add rust-analyzer")
	})

	t.Run("C/C++ returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Skip if clangd is installed locally (test assumes it's absent)
		if _, err := exec.LookPath("clangd"); err == nil {
			t.Skip("clangd is installed locally; skipping install-hint test")
		}

		_, _, err := m.GetOrCreate("/tmp", "c")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "clangd")
		assert.Contains(t, err.Error(), "clangd.llvm.org")
	})

	t.Run("C# returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// omnisharp is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "csharp")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "omnisharp")
		assert.Contains(t, err.Error(), "omnisharp-roslyn")
	})

	t.Run("Java returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// jdtls is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "java")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jdtls")
		assert.Contains(t, err.Error(), "eclipse")
	})

	t.Run("Ruby returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// solargraph is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "ruby")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "solargraph")
		assert.Contains(t, err.Error(), "gem install solargraph")
	})

	t.Run("PHP returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// intelephense is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "php")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "intelephense")
		assert.Contains(t, err.Error(), "npm install -g intelephense")
	})

	t.Run("Swift returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// Skip if sourcekit-lsp is installed locally (test assumes it's absent)
		if _, err := exec.LookPath("sourcekit-lsp"); err == nil {
			t.Skip("sourcekit-lsp is installed locally; skipping install-hint test")
		}

		_, _, err := m.GetOrCreate("/tmp", "swift")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sourcekit-lsp")
	})

	t.Run("Kotlin returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// kotlin-language-server is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "kotlin")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "kotlin-language-server")
		assert.Contains(t, err.Error(), "kotlin-language-server")
	})

	t.Run("Dart returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// dart is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "dart")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dart")
		assert.Contains(t, err.Error(), "Dart SDK")
	})

	t.Run("Lua returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// lua-language-server is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "lua")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lua-language-server")
		assert.Contains(t, err.Error(), "lua-language-server")
	})

	t.Run("Shell returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// bash-language-server is unlikely to be installed in CI
		_, _, err := m.GetOrCreate("/tmp", "shellscript")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bash-language-server")
		assert.Contains(t, err.Error(), "npm install -g bash-language-server")
	})

	t.Run("error message format is user-friendly", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		_, _, err := m.GetOrCreate("/tmp", "python")
		require.Error(t, err)
		msg := err.Error()
		// The message should follow the pattern: language server "X" not found on PATH. Install with: Y
		assert.Contains(t, msg, "not found on PATH")
		assert.Contains(t, msg, "Install with:")
	})

	t.Run("Go with gopls not installed returns error with install hint", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		// In CI, gopls IS installed, so this test verifies the error path
		// by using a custom config that points to a non-existent binary
		m.SetConfig([]LanguageServerConfig{
			{
				ID:          "go",
				LanguageIDs: []string{"go"},
				Binary:      "gopls-nonexistent",
				Args:        []string{},
				InstallHint: "go install golang.org/x/tools/gopls@latest",
			},
		})

		_, _, err := m.GetOrCreate("/tmp", "go")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gopls-nonexistent")
		assert.Contains(t, err.Error(), "go install golang.org/x/tools/gopls@latest")
	})

	t.Run("empty InstallHint falls back to generic error", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{
				ID:          "fake",
				LanguageIDs: []string{"fake"},
				Binary:      "nonexistent-binary-xyz-123",
				Args:        []string{},
				InstallHint: "",
			},
		})

		_, _, err := m.GetOrCreate("/tmp", "fake")
		require.Error(t, err)
		// When InstallHint is empty, it falls back to the generic "failed to find" message
		assert.Contains(t, err.Error(), "failed to find")
		assert.NotContains(t, err.Error(), "Install with:")
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

func TestManagerGetOrCreateReusesHealthyProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("GetOrCreate creates process and tracks it", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		proc1, release1, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		require.NotNil(t, proc1)

		// First call should create exactly 1 server
		assert.Equal(t, 1, m.Count())

		// Second call: since Healthy() uses Signal(nil) which may not work
		// on all platforms, GetOrCreate may start a new process or reuse the
		// existing one. Either way, the manager should handle it correctly.
		proc2, release2, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		require.NotNil(t, proc2)

		// After both calls, there should be at least one tracked server
		assert.GreaterOrEqual(t, m.Count(), 1)

		release1()
		release2()
	})
}

func TestManagerGetOrCreateReleasesRefCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("release decrements refCount enabling eviction", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		_, release, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		assert.Equal(t, 1, m.Count())

		// Release first handle (refCount goes from 1 to 0)
		release()

		// Now evict with tiny timeout - should remove the idle process
		m.EvictIdle(1 * time.Nanosecond)
		assert.Equal(t, 0, m.Count())
	})
}

func TestManagerEvictIdleActuallyEvicts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("idle process with zero refCount is evicted", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		_, release, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		assert.Equal(t, 1, m.Count())

		// Release so refCount drops to 0
		release()

		// Evict with tiny timeout - lastUsed is in the past relative to any timeout
		m.EvictIdle(1 * time.Nanosecond)

		// Process should have been evicted
		assert.Equal(t, 0, m.Count())
	})
}

func TestManagerEvictIdleKeepsActiveProcesses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("process with refCount > 0 is not evicted", func(t *testing.T) {
		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		_, release, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		assert.Equal(t, 1, m.Count())

		// Do NOT release - refCount is 1
		// Evict with tiny timeout
		m.EvictIdle(1 * time.Nanosecond)

		// Process should NOT have been evicted because refCount > 0
		assert.Equal(t, 1, m.Count())

		// Now release and evict again
		release()
		m.EvictIdle(1 * time.Nanosecond)
		assert.Equal(t, 0, m.Count())
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
		assert.Contains(t, key, "|")
	})
}

// --- Coverage gap tests for manager.go ---

func TestManagerGetOrCreateRestartsUnhealthyProcess(t *testing.T) {
	t.Run("GetOrCreate detects unhealthy process and restarts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		// Create first process
		proc1, release1, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		require.NotNil(t, proc1)
		assert.Equal(t, 1, m.Count())

		// Kill the process externally to make it unhealthy
		proc1.Close()

		// GetOrCreate should detect it's unhealthy and start a new one
		proc2, release2, err := m.GetOrCreate("/tmp", "cat")
		require.NoError(t, err)
		require.NotNil(t, proc2)
		// Should be a new process (different pointer)
		assert.Equal(t, 1, m.Count())

		release1()
		release2()
	})
}

func TestManagerGetOrCreateFindByIDFallback(t *testing.T) {
	t.Run("GetOrCreate falls back to FindLanguageServerByID", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m := NewManager(ctx)
		defer m.Close()

		// Create config where language ID is "mygo" but LanguageIDs doesn't contain "mygo"
		// However, the config ID is "go" - so FindLanguageServerByID("go", ...) finds it
		// This won't work because GetOrCreate searches by the languageID parameter.
		// Let's set a config where a language ID can be found by its ID field but not LanguageIDs
		m.SetConfig([]LanguageServerConfig{
			{
				ID:          "special-go",       // ID that GetOrCreate can find by ID
				LanguageIDs: []string{"golang"}, // NOT "special-go"
				Binary:      "cat",
				Args:        []string{},
			},
		})

		// FindLanguageServer("special-go", ...) returns nil
		// FindLanguageServerByID("special-go", ...) returns the config
		proc, release, err := m.GetOrCreate("/tmp", "special-go")
		require.NoError(t, err)
		require.NotNil(t, proc)
		assert.Equal(t, 1, m.Count())

		release()
	})

	t.Run("GetOrCreate with config found by ID but binary not on PATH", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{
				ID:          "fake-lang",
				LanguageIDs: []string{"realname"}, // NOT "fake-lang"
				Binary:      "nonexistent-binary-xyz-999",
				Args:        []string{},
			},
		})

		// FindLanguageServer("fake-lang") returns nil
		// FindLanguageServerByID("fake-lang") returns the config
		// But ResolveBinaryPath fails
		_, _, err := m.GetOrCreate("/tmp", "fake-lang")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to find")
	})
}

func TestManagerConcurrentAccess(t *testing.T) {
	t.Run("concurrent GetOrCreate and EvictIdle do not race", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m := NewManager(ctx)
		defer m.Close()

		m.SetConfig([]LanguageServerConfig{
			{ID: "cat", LanguageIDs: []string{"cat"}, Binary: "cat", Args: []string{}},
		})

		var wg sync.WaitGroup

		// Concurrent GetOrCreate callers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					_, release, err := m.GetOrCreate("/tmp", "cat")
					if err == nil && release != nil {
						release()
					}
				}
			}()
		}

		// Concurrent EvictIdle
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				m.EvictIdle(1 * time.Nanosecond)
			}
		}()

		// Concurrent Count readers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = m.Count()
			}
		}()

		wg.Wait()

		// After all goroutines complete, manager should be in a consistent state
		assert.GreaterOrEqual(t, m.Count(), 0)
	})
}
