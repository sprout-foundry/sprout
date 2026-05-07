package embedding

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// supportedFileExtensions lists file extensions that should be indexed
// at the file level for non-code files.
var supportedFileExtensions = map[string]bool{
	// Documentation
	".md":  true,
	".rst": true,
	".txt": true,

	// Configuration
	".yaml": true,
	".yml":  true,
	".toml": true,
	".json": true,
	".xml":  true,

	// Web/data
	".html": true,
	".css":  true,
	".sql":  true,

	// Shell scripts
	".sh":   true,
	".bash": true,
	".zsh":  true,
	".fish": true,

	// Config/build
	".env":    true,
	".cfg":    true,
	".ini":    true,
	".conf":   true,
	".gradle": true,
	".cmake":  true,
}

// FileExtractor produces file-level embeddings for non-code files.
type FileExtractor struct {
	maxFileBytes int
}

// NewFileExtractor creates a FileExtractor that truncates files larger than
// maxFileBytes. If maxFileBytes is 0, a default of 8000 bytes is used.
func NewFileExtractor(maxFileBytes int) *FileExtractor {
	if maxFileBytes <= 0 {
		maxFileBytes = 8000
	}
	return &FileExtractor{
		maxFileBytes: maxFileBytes,
	}
}

// Extract produces a single CodeUnit representing the entire file content.
// Returns an empty slice (no error) for unsupported file types.
func (e *FileExtractor) Extract(path string, content []byte) ([]CodeUnit, error) {
	ext := filepath.Ext(path)
	base := filepath.Base(path)

	// Check if this file type is supported
	if !e.isSupportedFile(path, ext, base) {
		return nil, nil
	}

	// Truncate content if needed
	body := string(content)
	if len(body) > e.maxFileBytes {
		// Truncate at the last valid UTF-8 boundary
		runes := []rune(body)
		if len(runes) > e.maxFileBytes {
			runes = runes[:e.maxFileBytes]
		}
		body = string(runes)
	}

	// Clean body based on file type
	body = e.cleanBody(ext, base, body)

	// Determine language
	lang := e.detectLanguage(ext, base)

	// Create single code unit for the entire file
	unit := CodeUnit{
		ID:        path, // Use file path as ID
		File:      path,
		Name:      base,
		Signature: base, // For file-level, signature is just the filename
		Body:      body,
		StartLine: 1,
		EndLine:   strings.Count(string(content), "\n") + 1,
		Language:  lang,
	}
	unit.ComputeHash()

	return []CodeUnit{unit}, nil
}

// isSupportedFile checks if the file is a supported non-code file type.
func (e *FileExtractor) isSupportedFile(path, ext, base string) bool {
	// Check special filenames (no extension) - uses package-level specialFilenames
	if specialFilenames[base] {
		return true
	}

	// Check supported extensions
	if supportedFileExtensions[ext] {
		return true
	}

	return false
}

// cleanBody processes content based on file type to remove noise.
func (e *FileExtractor) cleanBody(ext, base, body string) string {
	// For markdown: strip HTML-like tags, keep structure
	if ext == ".md" || base == "AGENTS.md" {
		body = cleanMarkdown(body)
	}

	return body
}

// cleanMarkdown removes HTML-like tags and excessive whitespace from markdown.
func cleanMarkdown(body string) string {
	// Remove HTML tags
	htmlTag := regexp.MustCompile(`<[^>]+>`)
	body = htmlTag.ReplaceAllString(body, "")

	// Collapse multiple newlines
	multiNewline := regexp.MustCompile(`\n{3,}`)
	body = multiNewline.ReplaceAllString(body, "\n\n")

	return strings.TrimSpace(body)
}

// detectLanguage determines the language identifier for the file.
func (e *FileExtractor) detectLanguage(ext, base string) string {
	switch {
	case ext == ".md" || base == "AGENTS.md":
		return "markdown"
	case ext == ".yaml" || ext == ".yml":
		return "yaml"
	case ext == ".json":
		return "json"
	case ext == ".toml":
		return "toml"
	case ext == ".xml":
		return "xml"
	case ext == ".css":
		return "css"
	case ext == ".html":
		return "html"
	case ext == ".sql":
		return "sql"
	case ext == ".sh" || ext == ".bash" || ext == ".zsh" || ext == ".fish":
		return "shell"
	case base == "Dockerfile" || base == ".dockerignore":
		return "dockerfile"
	case base == ".gitignore":
		return "config"
	case ext == ".env" || base == ".env.example":
		return "env"
	case ext == ".cfg" || ext == ".ini" || ext == ".conf":
		return "config"
	case ext == ".gradle":
		return "gradle"
	case ext == ".cmake":
		return "cmake"
	case ext == ".rst":
		return "rst"
	case ext == ".txt":
		return "text"
	default:
		return "text"
	}
}

// IsSupportedIndexableFile checks if a file path should be indexed at the file level.
// Used by the indexing pipeline to decide which files to process with FileExtractor.
func IsSupportedIndexableFile(path string) bool {
	ext := filepath.Ext(path)
	base := filepath.Base(path)

	// Check special filenames first - uses package-level specialFilenames
	if specialFilenames[base] {
		return true
	}

	// Check supported extensions
	return supportedFileExtensions[ext]
}

// HashContent computes a SHA-256 hex digest of the given content.
func HashContent(content []byte) string {
	h := sha256.New()
	h.Write(content)
	return fmt.Sprintf("%x", h.Sum(nil))
}
