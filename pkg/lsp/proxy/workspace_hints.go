package proxy

import (
	"os"
	"path/filepath"
)

// ServerSuggestion represents a recommended language server for a workspace.
type ServerSuggestion struct {
	Language    string // Language name (e.g. "go", "python")
	ProjectFile string // Detected project file (e.g. "Cargo.toml")
	ServerID    string // Matching server ID from DefaultLanguageServers
	InstallHint string // Installation instructions for the server
}

// projectDetectors defines the mapping from project file patterns to language server IDs.
// Each entry has a list of filenames/patterns to check and the corresponding server ID.
type projectDetector struct {
	Patterns []string // Filenames or glob patterns to look for
	ServerID string   // The language server ID to suggest
}

// Detect project files in the given workspace root and return suggestions.
func SuggestServers(workspaceRoot string) []ServerSuggestion {
	// Validate workspaceRoot exists and is a directory
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		return nil
	}
	workspaceRoot = absRoot

	detectors := []projectDetector{
		{Patterns: []string{"go.mod"}, ServerID: "go"},
		{Patterns: []string{"package.json", "package-lock.json"}, ServerID: "typescript"},
		{Patterns: []string{"requirements.txt", "pyproject.toml", "setup.py", "setup.cfg"}, ServerID: "python"},
		{Patterns: []string{"Cargo.toml"}, ServerID: "rust"},
		{Patterns: []string{"*.sln", "*.csproj"}, ServerID: "csharp"},
		{Patterns: []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}, ServerID: "java"},
		{Patterns: []string{"Gemfile", "Gemfile.lock"}, ServerID: "ruby"},
		{Patterns: []string{"composer.json"}, ServerID: "php"},
		{Patterns: []string{"Package.swift"}, ServerID: "swift"},
		{Patterns: []string{"pubspec.yaml"}, ServerID: "dart"},
		{Patterns: []string{"*.lua"}, ServerID: "lua"},
		{Patterns: []string{"*.sh", "*.bash", "*.zsh"}, ServerID: "shell"},
		{Patterns: []string{"CMakeLists.txt"}, ServerID: "c-cpp"},
		{Patterns: []string{"Makefile"}, ServerID: "c-cpp"},
	}

	var suggestions []ServerSuggestion
	defaultServers := DefaultLanguageServers()
	seen := make(map[string]bool)

	for _, detector := range detectors {
		var matchedFile string
		for _, pattern := range detector.Patterns {
			if containsDotPrefix(pattern) {
				continue // skip patterns that start with a dot (hidden files)
			}

			if pattern[0] == '*' {
				// Glob pattern
				matches, err := filepath.Glob(filepath.Join(workspaceRoot, pattern))
				if err != nil {
					continue
				}
				if len(matches) > 0 {
					matchedFile = filepath.Base(matches[0])
					break
				}
			} else {
				// Exact filename
				fullPath := filepath.Join(workspaceRoot, pattern)
				if _, err := os.Stat(fullPath); err == nil {
					matchedFile = pattern
					break
				}
			}
		}

		if matchedFile == "" {
			continue
		}

		if seen[detector.ServerID] {
			continue // Already suggested this server
		}
		seen[detector.ServerID] = true

		// Look up the server config
		serverConfig := FindLanguageServerByID(detector.ServerID, defaultServers)
		if serverConfig == nil {
			continue
		}

		// Determine the display language name
		language := serverConfig.ID
		if len(serverConfig.LanguageIDs) > 0 {
			language = serverConfig.LanguageIDs[0]
		}

		suggestions = append(suggestions, ServerSuggestion{
			Language:    language,
			ProjectFile: matchedFile,
			ServerID:    serverConfig.ID,
			InstallHint: serverConfig.InstallHint,
		})
	}

	return suggestions
}

// containsDotPrefix returns true if the pattern starts with a dot (indicating a hidden file).
func containsDotPrefix(pattern string) bool {
	base := filepath.Base(pattern)
	return len(base) > 0 && base[0] == '.'
}
