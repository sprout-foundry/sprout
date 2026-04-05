package agent

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

const resourceCaptureMaxSizeBytes = 50 * 1024 * 1024

func (a *Agent) resourceDirectory() string {
	if a == nil {
		return ""
	}

	raw := strings.TrimSpace(os.Getenv("LEDIT_RESOURCE_DIRECTORY"))
	if raw == "" {
		if cfg := a.GetConfig(); cfg != nil {
			raw = strings.TrimSpace(cfg.ResourceDirectory)
		}
	}
	if raw == "" {
		return ""
	}

	cleaned := filepath.Clean(raw)
	// Requirement: directory is always relative to current working directory.
	// If absolute is provided, normalize it into a relative path segment.
	if filepath.IsAbs(cleaned) {
		if vol := filepath.VolumeName(cleaned); vol != "" {
			cleaned = strings.TrimPrefix(cleaned, vol)
		}
		cleaned = strings.TrimLeft(cleaned, `/\`)
	}
	if cleaned == "." || cleaned == "" {
		return ""
	}

	return filepath.Join(a.currentWorkspaceRoot(), cleaned)
}

func (a *Agent) captureWebText(kind, source, text string) {
	dir := a.resourceDirectory()
	if dir == "" {
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}

	base := captureBaseName(kind, source)
	path := filepath.Join(dir, base+".txt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.debugLog("resource capture: failed to create directory %s: %v\n", dir, err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		a.debugLog("resource capture: failed writing %s: %v\n", path, err)
		return
	}
	a.appendResourceCaptureLog("saved_text", source, path, int64(len(text)), "")
}

func (a *Agent) captureVisionInputAndOutput(imagePath, rawResult string) {
	dir := a.resourceDirectory()
	if dir == "" || strings.TrimSpace(imagePath) == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.debugLog("resource capture: failed to create directory %s: %v\n", dir, err)
		return
	}

	if savedPath, size, err := a.captureVisionAsset(imagePath, dir); err != nil {
		a.debugLog("resource capture: failed asset capture for %s: %v\n", imagePath, err)
	} else if savedPath != "" {
		a.appendResourceCaptureLog("saved_asset", imagePath, savedPath, size, "")
	}

	extracted, meta := extractVisionTextAndMetadata(rawResult)
	if strings.TrimSpace(extracted) != "" {
		textPath := filepath.Join(dir, captureBaseName("vision_text", imagePath)+".txt")
		if err := os.WriteFile(textPath, []byte(extracted), 0o644); err != nil {
			a.debugLog("resource capture: failed writing OCR text %s: %v\n", textPath, err)
		} else {
			logMeta := map[string]interface{}{}
			if meta.OutputTruncated {
				logMeta["output_truncated"] = true
				logMeta["original_chars"] = meta.OriginalChars
				logMeta["returned_chars"] = meta.ReturnedChars
			}
			if meta.FullOutputPath != "" {
				logMeta["full_output_path"] = meta.FullOutputPath
			}
			a.appendResourceCaptureLogWithMeta("saved_text", imagePath, textPath, int64(len(extracted)), "", logMeta)
		}
	}

	if meta.FullOutputPath != "" {
		fullPath := meta.FullOutputPath
		if strings.HasPrefix(fullPath, "./") || strings.HasPrefix(fullPath, "../") {
			// Clean and resolve relative paths to prevent path traversal
			relativePath := filepath.Clean(filepath.FromSlash(meta.FullOutputPath))
			fullPath = filepath.Join(a.currentWorkspaceRoot(), relativePath)
		}
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			a.appendResourceCaptureLogWithMeta("saved_full_text", imagePath, meta.FullOutputPath, info.Size(), "", map[string]interface{}{
				"output_truncated": true,
			})
		}
	}
}

func (a *Agent) captureVisionAsset(imagePath, dir string) (string, int64, error) {
	if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
		return a.captureRemoteAsset(imagePath, dir)
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat vision asset %s: %w", imagePath, err)
	}
	if info.Size() > resourceCaptureMaxSizeBytes {
		a.appendResourceCaptureLog("skipped_large", imagePath, "", info.Size(), "asset exceeds 50MB limit")
		return "", info.Size(), nil
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read vision asset %s: %w", imagePath, err)
	}
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == "" {
		ext = ".bin"
	}
	out := filepath.Join(dir, captureBaseName("vision_asset", imagePath)+ext)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return "", 0, fmt.Errorf("failed to write vision asset to %s: %w", out, err)
	}
	return out, int64(len(data)), nil
}

func (a *Agent) captureRemoteAsset(source, dir string) (string, int64, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest("GET", source, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create HTTP request for %s: %w", source, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("HTTP request failed for %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > resourceCaptureMaxSizeBytes {
		a.appendResourceCaptureLog("skipped_large", source, "", resp.ContentLength, "asset exceeds 50MB limit")
		return "", resp.ContentLength, nil
	}

	limited := io.LimitReader(resp.Body, resourceCaptureMaxSizeBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read remote asset from %s: %w", source, err)
	}
	if int64(len(data)) > resourceCaptureMaxSizeBytes {
		a.appendResourceCaptureLog("skipped_large", source, "", int64(len(data)), "asset exceeds 50MB limit")
		return "", int64(len(data)), nil
	}

	ext := extensionFromSource(source)
	if ext == "" {
		ext = extensionFromContentType(resp.Header.Get("Content-Type"))
	}
	if ext == "" {
		ext = ".bin"
	}

	out := filepath.Join(dir, captureBaseName("vision_asset", source)+ext)
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return "", 0, fmt.Errorf("failed to write remote asset to %s: %w", out, err)
	}
	return out, int64(len(data)), nil
}

func (a *Agent) appendResourceCaptureLog(action, source, path string, size int64, note string) {
	a.appendResourceCaptureLogWithMeta(action, source, path, size, note, nil)
}

func (a *Agent) appendResourceCaptureLogWithMeta(action, source, path string, size int64, note string, meta map[string]interface{}) {
	dir := a.resourceDirectory()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.debugLog("resource capture: failed to create log directory %s: %v\n", dir, err)
		return
	}
	entry := map[string]interface{}{
		"ts":     time.Now().UTC().Format(time.RFC3339),
		"action": action,
		"source": source,
		"path":   path,
		"size":   size,
		"note":   note,
	}
	for k, v := range meta {
		entry[k] = v
	}
	blob, _ := json.Marshal(entry)
	logPath := filepath.Join(dir, "resource_capture.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		a.debugLog("resource capture: failed opening log %s: %v\n", logPath, err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(blob, '\n'))
}

func captureBaseName(kind, source string) string {
	source = strings.TrimSpace(source)
	hash := sha1.Sum([]byte(source))
	shortHash := hex.EncodeToString(hash[:])[:12]
	stem := sanitizeFileComponent(source)
	if stem == "" {
		stem = "resource"
	}
	if len(stem) > 80 {
		stem = stem[:80]
	}
	return fmt.Sprintf("%s_%s_%s", kind, stem, shortHash)
}

func sanitizeFileComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"?", "_",
		"&", "_",
		"=", "_",
		" ", "_",
		"\n", "_",
		"\r", "_",
	).Replace(s)
	return strings.Trim(s, "._-")
}

func extensionFromSource(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return strings.ToLower(filepath.Ext(source))
	}
	return strings.ToLower(filepath.Ext(u.Path))
}

func extensionFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	switch {
	case strings.Contains(ct, "pdf"):
		return ".pdf"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "text/plain"):
		return ".txt"
	default:
		return ""
	}
}

type visionCaptureMetadata struct {
	OutputTruncated bool
	OriginalChars   int
	ReturnedChars   int
	FullOutputPath  string
}

func extractVisionTextAndMetadata(rawResult string) (string, visionCaptureMetadata) {
	meta := visionCaptureMetadata{}
	rawResult = strings.TrimSpace(rawResult)
	if rawResult == "" || !strings.HasPrefix(rawResult, "{") {
		return rawResult, meta
	}
	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(rawResult), &parsed); err != nil {
		return rawResult, meta
	}
	meta.OutputTruncated = parsed.OutputTruncated
	meta.OriginalChars = parsed.OriginalChars
	meta.ReturnedChars = parsed.ReturnedChars
	meta.FullOutputPath = strings.TrimSpace(parsed.FullOutputPath)

	if txt := strings.TrimSpace(parsed.ExtractedText); txt != "" {
		return txt, meta
	}
	if parsed.Analysis != nil {
		if desc := strings.TrimSpace(parsed.Analysis.Description); desc != "" {
			return desc, meta
		}
	}
	// Preserve raw JSON if no extracted field to keep debuggability.
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(parsed); err == nil {
		return strings.TrimSpace(out.String()), meta
	}
	return rawResult, meta
}
