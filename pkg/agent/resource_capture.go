package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const resourceCaptureMaxSizeBytes = 50 * 1024 * 1024

func (a *Agent) resourceDirectory() string {
	if a == nil {
		return ""
	}

	raw := strings.TrimSpace(configuration.GetEnvSimple("RESOURCE_DIRECTORY"))
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
		a.Logger().Debug("resource capture: failed to create directory %s: %v\n", dir, err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		a.Logger().Debug("resource capture: failed writing %s: %v\n", path, err)
		return
	}
	a.appendResourceCaptureLog("saved_text", source, path, int64(len(text)), "")
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
		a.Logger().Debug("resource capture: failed to create log directory %s: %v\n", dir, err)
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
		a.Logger().Debug("resource capture: failed opening log %s: %v\n", logPath, err)
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
