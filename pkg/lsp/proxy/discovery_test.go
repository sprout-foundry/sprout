package proxy

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBinaryPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows due to different binary behavior")
	}

	t.Run("known binary exists", func(t *testing.T) {
		// Test with a binary that should exist on most Unix systems
		path, err := ResolveBinaryPath("ls")
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("echo binary exists", func(t *testing.T) {
		// echo is a standard Unix utility
		path, err := ResolveBinaryPath("echo")
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("cat binary exists", func(t *testing.T) {
		// cat is another standard Unix utility
		path, err := ResolveBinaryPath("cat")
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("sh binary exists", func(t *testing.T) {
		// sh is commonly a symlink to bash or dash
		path, err := ResolveBinaryPath("sh")
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")

		// On many systems, sh is a symlink
		// The path should be resolved to the real path
		assert.NotContains(t, path, "..", "path should not contain ..")
	})

	t.Run("nonexistent binary", func(t *testing.T) {
		// Test with a binary that definitely doesn't exist
		path, err := ResolveBinaryPath("nonexistent-binary-xyz-123")
		require.Error(t, err)
		assert.Empty(t, path)
		assert.Contains(t, err.Error(), "not found on PATH")
	})

	t.Run("empty binary name", func(t *testing.T) {
		path, err := ResolveBinaryPath("")
		require.Error(t, err)
		assert.Empty(t, path)
	})
}

func TestResolveBinaryPathWithGo(t *testing.T) {
	// Test with the go binary which should be available in the test environment
	path, err := ResolveBinaryPath("go")
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.True(t, filepath.IsAbs(path), "path should be absolute")
	assert.Contains(t, strings.ToLower(path), "go", "path should contain 'go'")
}

func TestResolveBinaryPathWithGit(t *testing.T) {
	// Test with git which should be available since this is a git repo
	path, err := ResolveBinaryPath("git")
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.True(t, filepath.IsAbs(path), "path should be absolute")
}

func TestFindLanguageServer(t *testing.T) {
	configs := DefaultLanguageServers()

	t.Run("find Go language server", func(t *testing.T) {
		cfg := FindLanguageServer("go", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "go", cfg.ID)
		assert.Equal(t, "gopls", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "go")
	})

	t.Run("find TypeScript language server", func(t *testing.T) {
		cfg := FindLanguageServer("typescript", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "typescript", cfg.ID)
		assert.Equal(t, "typescript-language-server", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "typescript")
	})

	t.Run("find JavaScript language server", func(t *testing.T) {
		cfg := FindLanguageServer("javascript", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "typescript", cfg.ID) // Uses same server as TS
		assert.Contains(t, cfg.LanguageIDs, "javascript")
	})

	t.Run("find TypeScript JSX language server", func(t *testing.T) {
		cfg := FindLanguageServer("typescript-jsx", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "typescript", cfg.ID)
		assert.Contains(t, cfg.LanguageIDs, "typescript-jsx")
	})

	t.Run("find JavaScript JSX language server", func(t *testing.T) {
		cfg := FindLanguageServer("javascript-jsx", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "typescript", cfg.ID)
		assert.Contains(t, cfg.LanguageIDs, "javascript-jsx")
	})

	t.Run("unknown language", func(t *testing.T) {
		cfg := FindLanguageServer("unknown-language", configs)
		assert.Nil(t, cfg)
	})

	t.Run("case sensitive search", func(t *testing.T) {
		// The search in FindLanguageServer is case-sensitive
		cfg := FindLanguageServer("GO", configs)
		assert.Nil(t, cfg)
	})

	t.Run("empty language ID", func(t *testing.T) {
		cfg := FindLanguageServer("", configs)
		assert.Nil(t, cfg)
	})

	t.Run("search with empty configs", func(t *testing.T) {
		cfg := FindLanguageServer("go", []LanguageServerConfig{})
		assert.Nil(t, cfg)
	})
}

func TestFindLanguageServerByID(t *testing.T) {
	configs := DefaultLanguageServers()

	t.Run("find Go by ID", func(t *testing.T) {
		cfg := FindLanguageServerByID("go", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "go", cfg.ID)
		assert.Equal(t, "gopls", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "go")
	})

	t.Run("find TypeScript by ID", func(t *testing.T) {
		cfg := FindLanguageServerByID("typescript", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "typescript", cfg.ID)
		assert.Equal(t, "typescript-language-server", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "typescript")
	})

	t.Run("unknown ID", func(t *testing.T) {
		cfg := FindLanguageServerByID("unknown-id", configs)
		assert.Nil(t, cfg)
	})

	t.Run("case sensitive search", func(t *testing.T) {
		// ID search should be case-sensitive
		cfg := FindLanguageServerByID("GO", configs)
		assert.Nil(t, cfg)
	})

	t.Run("empty ID", func(t *testing.T) {
		cfg := FindLanguageServerByID("", configs)
		assert.Nil(t, cfg)
	})

	t.Run("search with empty configs", func(t *testing.T) {
		cfg := FindLanguageServerByID("go", []LanguageServerConfig{})
		assert.Nil(t, cfg)
	})
}

func TestNormalizeLanguageID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase",
			input: "GO",
			want:  "go",
		},
		{
			name:  "already lowercase",
			input: "go",
			want:  "go",
		},
		{
			name:  "uppercase",
			input: "TYPESCRIPT",
			want:  "typescript",
		},
		{
			name:  "mixed case",
			input: "TypeScript",
			want:  "typescript",
		},
		{
			name:  "with leading spaces",
			input: "  go",
			want:  "go",
		},
		{
			name:  "with trailing spaces",
			input: "go  ",
			want:  "go",
		},
		{
			name:  "with leading and trailing spaces",
			input: "  go  ",
			want:  "go",
		},
		{
			name:  "with multiple spaces",
			input: "   go   ",
			want:  "go",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only spaces",
			input: "   ",
			want:  "",
		},
		{
			name:  "with tabs",
			input: "\tgo\t",
			want:  "go",
		},
		{
			name:  "mixed whitespace",
			input: " \t go \t ",
			want:  "go",
		},
		{
			name:  "with hyphen",
			input: "TYPESCRIPT-JSX",
			want:  "typescript-jsx",
		},
		{
			name:  "with special characters preserved",
			input: "  C++  ",
			want:  "c++",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeLanguageID(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultLanguageServers(t *testing.T) {
	configs := DefaultLanguageServers()

	t.Run("returns configs", func(t *testing.T) {
		assert.NotEmpty(t, configs)
		assert.Greater(t, len(configs), 0)
	})

	t.Run("has Go config", func(t *testing.T) {
		goCfg := FindLanguageServerByID("go", configs)
		require.NotNil(t, goCfg)
		assert.Equal(t, "go", goCfg.ID)
		assert.Equal(t, "gopls", goCfg.Binary)
		assert.NotNil(t, goCfg.Args)
	})

	t.Run("has TypeScript config", func(t *testing.T) {
		tsCfg := FindLanguageServerByID("typescript", configs)
		require.NotNil(t, tsCfg)
		assert.Equal(t, "typescript", tsCfg.ID)
		assert.Equal(t, "typescript-language-server", tsCfg.Binary)
		assert.NotEmpty(t, tsCfg.Args)
		assert.Contains(t, tsCfg.Args, "--stdio")
	})

	t.Run("returns exactly 14 configs", func(t *testing.T) {
		assert.Equal(t, 14, len(configs), "should have 14 language server configs")
	})

	t.Run("no duplicate IDs", func(t *testing.T) {
		seen := make(map[string]bool)
		for _, cfg := range configs {
			assert.False(t, seen[cfg.ID], "duplicate language server ID: %s", cfg.ID)
			seen[cfg.ID] = true
		}
	})

	t.Run("all configs have non-empty InstallHint", func(t *testing.T) {
		for _, cfg := range configs {
			assert.NotEmpty(t, cfg.InstallHint, "config %q should have a non-empty InstallHint", cfg.ID)
		}
	})

	t.Run("Python config", func(t *testing.T) {
		cfg := FindLanguageServerByID("python", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "python", cfg.ID)
		assert.Equal(t, "pylsp", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "python")
		assert.Equal(t, "pip install python-lsp-server", cfg.InstallHint)
	})

	t.Run("Rust config", func(t *testing.T) {
		cfg := FindLanguageServerByID("rust", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "rust", cfg.ID)
		assert.Equal(t, "rust-analyzer", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "rust")
		assert.Equal(t, "rustup component add rust-analyzer", cfg.InstallHint)
	})

	t.Run("C/C++ config", func(t *testing.T) {
		cfg := FindLanguageServerByID("c-cpp", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "c-cpp", cfg.ID)
		assert.Equal(t, "clangd", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "c")
		assert.Contains(t, cfg.LanguageIDs, "cpp")
		assert.Contains(t, cfg.LanguageIDs, "c-cpp")
		assert.Contains(t, cfg.InstallHint, "clangd.llvm.org")
	})

	t.Run("C# config", func(t *testing.T) {
		cfg := FindLanguageServerByID("csharp", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "csharp", cfg.ID)
		assert.Equal(t, "omnisharp", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "csharp")
		assert.Contains(t, cfg.Args, "-lsp")
		assert.Contains(t, cfg.InstallHint, "omnisharp")
	})

	t.Run("Java config", func(t *testing.T) {
		cfg := FindLanguageServerByID("java", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "java", cfg.ID)
		assert.Equal(t, "jdtls", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "java")
		assert.Contains(t, cfg.InstallHint, "eclipse")
	})

	t.Run("Ruby config", func(t *testing.T) {
		cfg := FindLanguageServerByID("ruby", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "ruby", cfg.ID)
		assert.Equal(t, "solargraph", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "ruby")
		assert.Contains(t, cfg.Args, "stdio")
		assert.Equal(t, "gem install solargraph", cfg.InstallHint)
	})

	t.Run("PHP config", func(t *testing.T) {
		cfg := FindLanguageServerByID("php", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "php", cfg.ID)
		assert.Equal(t, "intelephense", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "php")
		assert.Contains(t, cfg.Args, "--stdio")
		assert.Contains(t, cfg.InstallHint, "npm install -g intelephense")
	})

	t.Run("Swift config", func(t *testing.T) {
		cfg := FindLanguageServerByID("swift", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "swift", cfg.ID)
		assert.Equal(t, "sourcekit-lsp", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "swift")
		assert.Contains(t, cfg.InstallHint, "sourcekit-lsp")
	})

	t.Run("Kotlin config", func(t *testing.T) {
		cfg := FindLanguageServerByID("kotlin", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "kotlin", cfg.ID)
		assert.Equal(t, "kotlin-language-server", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "kotlin")
		assert.Contains(t, cfg.InstallHint, "kotlin-language-server")
	})

	t.Run("Dart config", func(t *testing.T) {
		cfg := FindLanguageServerByID("dart", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "dart", cfg.ID)
		assert.Equal(t, "dart", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "dart")
		assert.Contains(t, cfg.Args, "language-server")
		assert.Contains(t, cfg.Args, "--protocol=lsp")
		assert.Contains(t, cfg.InstallHint, "Dart SDK")
	})

	t.Run("Lua config", func(t *testing.T) {
		cfg := FindLanguageServerByID("lua", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "lua", cfg.ID)
		assert.Equal(t, "lua-language-server", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "lua")
		assert.Contains(t, cfg.InstallHint, "lua-language-server")
	})

	t.Run("Shell config", func(t *testing.T) {
		cfg := FindLanguageServerByID("shell", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "shell", cfg.ID)
		assert.Equal(t, "bash-language-server", cfg.Binary)
		assert.Contains(t, cfg.LanguageIDs, "shellscript")
		assert.Contains(t, cfg.LanguageIDs, "bash")
		assert.Contains(t, cfg.LanguageIDs, "sh")
		assert.Contains(t, cfg.Args, "start")
		assert.Contains(t, cfg.InstallHint, "npm install -g bash-language-server")
	})

	t.Run("Go config has InstallHint", func(t *testing.T) {
		cfg := FindLanguageServerByID("go", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "go install golang.org/x/tools/gopls@latest", cfg.InstallHint)
	})

	t.Run("TypeScript config has InstallHint", func(t *testing.T) {
		cfg := FindLanguageServerByID("typescript", configs)
		require.NotNil(t, cfg)
		assert.Equal(t, "npm install -g typescript-language-server typescript", cfg.InstallHint)
	})
}

func TestDefaultLanguageServersFindLanguageServer(t *testing.T) {
	// Verify FindLanguageServer works for ALL language IDs across all 14 configs
	configs := DefaultLanguageServers()

	expected := map[string]string{
		// Go
		"go": "go",
		// TypeScript/JavaScript
		"typescript":      "typescript",
		"typescript-jsx":  "typescript",
		"javascript":      "typescript",
		"javascript-jsx":  "typescript",
		// Python
		"python": "python",
		// Rust
		"rust": "rust",
		// C/C++
		"c":     "c-cpp",
		"cpp":   "c-cpp",
		"c-cpp": "c-cpp",
		// C#
		"csharp": "csharp",
		// Java
		"java": "java",
		// Ruby
		"ruby": "ruby",
		// PHP
		"php": "php",
		// Swift
		"swift": "swift",
		// Kotlin
		"kotlin": "kotlin",
		// Dart
		"dart": "dart",
		// Lua
		"lua": "lua",
		// Shell
		"shellscript": "shell",
		"bash":        "shell",
		"sh":          "shell",
	}

	for langID, expectedConfigID := range expected {
		t.Run("FindLanguageServer/"+langID, func(t *testing.T) {
			cfg := FindLanguageServer(langID, configs)
			require.NotNil(t, cfg, "FindLanguageServer(%q) should return a config", langID)
			assert.Equal(t, expectedConfigID, cfg.ID, "FindLanguageServer(%q) should return config %q", langID, expectedConfigID)
		})
	}
}

func TestResolveBinaryPathSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink resolution test on Windows")
	}

	// Test symlink resolution with sh which is often a symlink
	path, err := ResolveBinaryPath("sh")
	require.NoError(t, err)

	// Verify we got an absolute path
	assert.True(t, filepath.IsAbs(path), "path should be absolute")

	// Try to read the symlink info
	realPath, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	// The returned path should be the resolved path (no symlinks)
	// This is what ResolveBinaryPath is supposed to do
	assert.Equal(t, path, realPath, "path should already be resolved")
}

// --- Coverage gap tests for discovery.go ---

// TestResolveBinaryPathEvalSymlinkFallback tests the EvalSymlinks failure path.
// This is difficult to trigger directly since filepath.EvalSymlinks rarely fails
// on valid paths. We can't easily force this condition without modifying production code.
// The path is: ResolveBinaryPath calls EvalSymlinks, if it fails, returns original path.
func TestResolveBinaryPathEvalSymlinkFallbackImpossible(t *testing.T) {
	t.Run("normal binary still returns resolved path", func(t *testing.T) {
		// Verify normal operation - EvalSymlinks succeeds
		path, err := ResolveBinaryPath("cat")
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(path))
	})
}

func TestResolveBinaryPathConsistency(t *testing.T) {
	// Test that ResolveBinaryPath returns consistent results
	// for the same binary across multiple calls
	binaries := []string{"ls", "cat", "echo"}

	for _, binary := range binaries {
		t.Run(binary, func(t *testing.T) {
			path1, err1 := ResolveBinaryPath(binary)
			require.NoError(t, err1)

			path2, err2 := ResolveBinaryPath(binary)
			require.NoError(t, err2)

			assert.Equal(t, path1, path2, "paths should be consistent")
		})
	}
}
