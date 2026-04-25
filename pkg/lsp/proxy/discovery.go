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
}

// DefaultLanguageServers returns the built-in language server configurations.
// Only include languages where we have a clear server story:
// - Go: gopls (stdio)
// - TypeScript/JS: typescript-language-server --stdio
//
// IMPORTANT: gopls v0.17+ requires `gopls` binary on PATH with `--listen` not used - use stdio.
// For typescript-language-server, it requires the binary on PATH + `--stdio`.
func DefaultLanguageServers() []LanguageServerConfig {
	return []LanguageServerConfig{
		{
			ID:          "go",
			LanguageIDs: []string{"go"},
			Binary:      "gopls",
			Args:        []string{},
		},
		{
			ID:          "typescript",
			LanguageIDs: []string{"typescript", "typescript-jsx", "javascript", "javascript-jsx"},
			Binary:      "typescript-language-server",
			Args:        []string{"--stdio"},
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