package proxy

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// LanguageServerConfig describes how to find and start a language server.
type LanguageServerConfig struct {
	LanguageIDs []string // e.g. ["go"], ["typescript", "typescript-jsx", "javascript", "javascript-jsx"]
	Binary      string   // "gopls", "typescript-language-server", etc.
	Args        []string // e.g. ["--stdio"]
	ID          string   // "go", "typescript"
	InstallHint string   // e.g. "pip install python-lsp-server"
}

// DefaultLanguageServers returns the built-in language server configurations.
// Covers Go, TypeScript/JS, Python, Rust, C/C++, C#, Java, Ruby, PHP,
// Swift, Kotlin, Dart, Lua, and Shell.
//
// IMPORTANT: gopls v0.17+ requires `gopls` binary on PATH with `--listen` not used - use stdio.
// For typescript-language-server, it requires the binary on PATH + `--stdio`.
func DefaultLanguageServers() []LanguageServerConfig {
	return []LanguageServerConfig{
		// Go
		{
			ID:          "go",
			LanguageIDs: []string{"go"},
			Binary:      "gopls",
			Args:        []string{},
			InstallHint: "go install golang.org/x/tools/gopls@latest",
		},
		// TypeScript/JavaScript
		{
			ID:          "typescript",
			LanguageIDs: []string{"typescript", "typescript-jsx", "javascript", "javascript-jsx"},
			Binary:      "typescript-language-server",
			Args:        []string{"--stdio"},
			InstallHint: "npm install -g typescript-language-server typescript",
		},
		// Python
		{
			ID:          "python",
			LanguageIDs: []string{"python"},
			Binary:      "pylsp",
			Args:        []string{},
			InstallHint: "pip install python-lsp-server",
		},
		// Rust
		{
			ID:          "rust",
			LanguageIDs: []string{"rust"},
			Binary:      "rust-analyzer",
			Args:        []string{},
			InstallHint: "rustup component add rust-analyzer",
		},
		// C/C++
		{
			ID:          "c-cpp",
			LanguageIDs: []string{"c", "cpp", "c-cpp"},
			Binary:      "clangd",
			Args:        []string{},
			InstallHint: "See https://clangd.llvm.org/installation.html",
		},
		// C#
		{
			ID:          "csharp",
			LanguageIDs: []string{"csharp"},
			Binary:      "omnisharp",
			Args:        []string{"-lsp"},
			InstallHint: "See https://github.com/OmniSharp/omnisharp-roslyn",
		},
		// Java
		{
			ID:          "java",
			LanguageIDs: []string{"java"},
			Binary:      "jdtls",
			Args:        []string{},
			InstallHint: "See https://github.com/eclipse-jdtls/eclipse.jdt.ls",
		},
		// Ruby
		{
			ID:          "ruby",
			LanguageIDs: []string{"ruby"},
			Binary:      "solargraph",
			Args:        []string{"stdio"},
			InstallHint: "gem install solargraph",
		},
		// PHP
		{
			ID:          "php",
			LanguageIDs: []string{"php"},
			Binary:      "intelephense",
			Args:        []string{"--stdio"},
			InstallHint: "npm install -g intelephense",
		},
		// Swift
		{
			ID:          "swift",
			LanguageIDs: []string{"swift"},
			Binary:      "sourcekit-lsp",
			Args:        []string{},
			InstallHint: "brew install sourcekit-lsp or see https://github.com/apple/sourcekit-lsp",
		},
		// Kotlin
		{
			ID:          "kotlin",
			LanguageIDs: []string{"kotlin"},
			Binary:      "kotlin-language-server",
			Args:        []string{},
			InstallHint: "See https://github.com/fwcd/kotlin-language-server",
		},
		// Dart
		{
			ID:          "dart",
			LanguageIDs: []string{"dart"},
			Binary:      "dart",
			Args:        []string{"language-server", "--protocol=lsp"},
			InstallHint: "Included with the Dart SDK",
		},
		// Lua
		{
			ID:          "lua",
			LanguageIDs: []string{"lua"},
			Binary:      "lua-language-server",
			Args:        []string{},
			InstallHint: "See https://github.com/LuaLS/lua-language-server",
		},
		// Shell
		{
			ID:          "shell",
			LanguageIDs: []string{"shellscript", "bash", "sh"},
			Binary:      "bash-language-server",
			Args:        []string{"start"},
			InstallHint: "npm install -g bash-language-server",
		},
	}
}

// FindLanguageServer finds a language server configuration by language ID.
// Returns nil if not found.
func FindLanguageServer(languageID string, configs []LanguageServerConfig) *LanguageServerConfig {
	for _, cfg := range configs {
		for _, id := range cfg.LanguageIDs {
			if id == languageID {
				return &cfg
			}
		}
	}
	return nil
}

// FindLanguageServerByID finds a language server configuration by its unique ID.
// Returns nil if not found.
func FindLanguageServerByID(id string, configs []LanguageServerConfig) *LanguageServerConfig {
	for _, cfg := range configs {
		if cfg.ID == id {
			return &cfg
		}
	}
	return nil
}

// ResolveBinaryPath resolves the full path to a binary on PATH.
// Returns an error if not found.
func ResolveBinaryPath(binary string) (string, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH: %w", binary, err)
	}
	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, nil // Return original path if symlink resolution fails
	}
	return realPath, nil
}

// NormalizeLanguageID normalizes a language ID string (lowercase, trim whitespace).
func NormalizeLanguageID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
