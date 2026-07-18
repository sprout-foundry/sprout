package tools

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// vision_utils.go — Vision output & file utilities.
// This file contains three concerns:
//  1. Output-text truncation (limitVisionOutputText)
//  2. Vision-output persistence helpers (persistVisionFullText*, resolveVisionOutputDirectory*, sanitizeVisionFileComponent)
//  3. Small file/path utilities (classifyPDFProcessingErrorCode, GetFileExtension, GetBaseName, IsHTMLInput)

// ============================================================================
// Output Text Handling
// ============================================================================

func limitVisionOutputText(text string) (string, bool, int) {
	trimmed := strings.TrimSpace(text)
	original := len(trimmed)
	maxChars := getVisionMaxReturnedTextChars()
	if original <= maxChars {
		return trimmed, false, original
	}

	suffix := fmt.Sprintf("\n\n[TRUNCATED: returned first %d of %d characters]", maxChars, original)
	keep := maxChars - len(suffix)
	if keep < 0 {
		keep = maxChars
		suffix = ""
	}
	return strings.TrimSpace(trimmed[:keep]) + suffix, true, original
}

// ============================================================================
// Persistence Helpers
// ============================================================================

// persistVisionFullTextWithRoot persists full vision text to a file rooted at workspaceRoot.
// If workspaceRoot is empty, it falls back to os.Getwd() for the CWD-based output dir.
// The relative path for display also uses workspaceRoot (or os.Getwd() as fallback).
func persistVisionFullTextWithRoot(sourcePath, fullText, workspaceRoot string) (string, error) {
	fullText = strings.TrimSpace(fullText)
	if fullText == "" {
		return "", fmt.Errorf("full text is empty")
	}

	dir := resolveVisionOutputDirectoryWithRoot(workspaceRoot)
	if dir == "" {
		return "", fmt.Errorf("vision output directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create vision output directory: %w", err)
	}

	base := sanitizeVisionFileComponent(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
	if base == "" {
		base = "vision_output"
	}
	hash := sha1.Sum([]byte(strings.TrimSpace(sourcePath)))
	shortHash := hex.EncodeToString(hash[:])[:12]
	fileName := fmt.Sprintf("%s_%s_full.txt", base, shortHash)
	fullPath := filepath.Join(dir, fileName)

	if err := os.WriteFile(fullPath, []byte(fullText), 0o644); err != nil {
		return "", fmt.Errorf("write full vision output: %w", err)
	}

	wd := workspaceRoot
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return fullPath, nil
		}
	}
	rel, err := filepath.Rel(wd, fullPath)
	if err != nil {
		return fullPath, nil
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return fullPath, nil
	}
	return "./" + rel, nil
}

// persistVisionFullText persists full vision text to a file using os.Getwd() for directory resolution.
// Kept for backward compatibility; new code should prefer persistVisionFullTextWithRoot.
func persistVisionFullText(sourcePath, fullText string) (string, error) {
	return persistVisionFullTextWithRoot(sourcePath, fullText, "")
}

// resolveVisionOutputDirectoryWithRoot resolves the vision output directory using the given
// workspace root. If workspaceRoot is empty, it falls back to os.Getwd().
func resolveVisionOutputDirectoryWithRoot(workspaceRoot string) string {
	raw := strings.TrimSpace(configuration.GetEnvSimple("RESOURCE_DIRECTORY"))
	if raw == "" {
		raw = ".sprout_ocr_outputs"
	}
	cleaned := filepath.Clean(raw)
	if filepath.IsAbs(cleaned) {
		if vol := filepath.VolumeName(cleaned); vol != "" {
			cleaned = strings.TrimPrefix(cleaned, vol)
		}
		cleaned = strings.TrimLeft(cleaned, `/\`)
	}
	wd := workspaceRoot
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(wd, cleaned)
}

// resolveVisionOutputDirectory resolves the vision output directory using os.Getwd().
// Kept for backward compatibility; new code should prefer resolveVisionOutputDirectoryWithRoot.
func resolveVisionOutputDirectory() string {
	return resolveVisionOutputDirectoryWithRoot("")
}

func sanitizeVisionFileComponent(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// ============================================================================
// File & Path Utilities
// ============================================================================

func classifyPDFProcessingErrorCode(err error) string {
	if err == nil {
		return ErrCodePDFProcessingFailed
	}

	// Prefer typed-error chain: extract *TypedError and map its code.
	var te *agenterrors.TypedError
	if errors.As(err, &te) {
		if code := typedErrorToVisionCode(te); code != "" {
			return code
		}
	}

	// Legacy fallback for untyped errors.
	return legacyClassifyPDFError(err)
}

// legacyClassifyPDFError is the pre-typed-error strings.Contains classifier
// for PDF processing errors. Preserved verbatim so behavior for untyped errors
// does not regress.
func legacyClassifyPDFError(err error) string {
	msg := strings.ToLower(err.Error())

	// Input path / retrieval failures.
	if strings.Contains(msg, "download pdf") ||
		strings.Contains(msg, "status 404") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "status 401") {
		return ErrCodeRemoteFetchFailed
	}
	if strings.Contains(msg, "stat pdf file") ||
		strings.Contains(msg, "no such file or directory") {
		return ErrCodeLocalFileNotFound
	}

	// Provider/inference transport failures: model call failed, not PDF support.
	if strings.Contains(msg, "ocr request") ||
		strings.Contains(msg, "http 5") ||
		strings.Contains(msg, "http 4") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "create vision client") ||
		strings.Contains(msg, "no response from ocr model") {
		return ErrCodeVisionRequestFailed
	}

	// Invalid/unsupported PDF content.
	if strings.Contains(msg, "missing %pdf header") ||
		strings.Contains(msg, "not a valid pdf") {
		return ErrCodeInputUnsupported
	}

	// Conservative default for unknown PDF failures.
	return ErrCodePDFProcessingFailed
}

// GetFileExtension returns the file extension (with dot) in lowercase
func GetFileExtension(path string) string {
	ext := filepath.Ext(path)
	return strings.ToLower(ext)
}

// GetBaseName returns the base name of a file path
func GetBaseName(path string) string {
	return filepath.Base(path)
}

// IsHTMLInput checks if the input path appears to be HTML content.
// For URLs, it does a HEAD request to check Content-Type.
// For local files, it checks the file extension.
func IsHTMLInput(path string) bool {
	lp := strings.ToLower(path)
	if strings.HasPrefix(lp, "http://") || strings.HasPrefix(lp, "https://") {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Head(path)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
	}
	ext := strings.ToLower(GetFileExtension(path))
	return ext == ".html" || ext == ".htm"
}
