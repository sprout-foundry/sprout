//go:build !js

package webui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// excludedDirNames lists directory names that are skipped during workspace
// hydration scans. These are build artifacts, dependency caches, and VCS
// data that are not needed in the OPFS replica.
var excludedDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	".DS_Store":    true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	".cache":       true,
}

// excludedBinaryExtensions lists file extensions that are considered binary.
// These files are still transferred, but those larger than 1MB are skipped.
var excludedBinaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true,
	".eot": true, ".zip": true, ".tar": true, ".gz": true,
	".rar": true, ".7z": true, ".mp3": true, ".mp4": true,
	".avi": true, ".mov": true, ".wmv": true, ".wasm": true,
	".sqlite": true, ".db": true,
}

// sensitiveFileNames lists filenames that should never be transmitted during
// hydration because they may contain secrets or credentials.
var sensitiveFileNames = map[string]bool{
	".env": true, ".env.local": true, ".env.production": true, ".env.development": true,
	".env.staging": true, ".env.test": true,
	".npmrc": true, ".pypirc": true, ".netrc": true,
}

// sensitiveFileExts lists file extensions that indicate the file contains
// secrets or credentials and should be excluded from hydration.
var sensitiveFileExts = map[string]bool{
	".pem": true, ".key": true, ".p12": true, ".pfx": true,
	".keystore": true, ".jks": true,
}

const (
	// maxFileSize is the maximum size of a single file that will be
	// transferred during hydration. Files larger than this are skipped.
	maxFileSize = 10 * 1024 * 1024 // 10MB

	// maxBinaryFileSize is the maximum size for binary files. Binary
	// files larger than this are skipped even if they're under maxFileSize.
	maxBinaryFileSize = 1 * 1024 * 1024 // 1MB

	// hydrateFileBatchSize is the number of files sent per batch before
	// a small pause to allow the event loop to breathe.
	hydrateFileBatchSize = 100

	// hydrateBatchPause is the pause between file batches during hydration.
	hydrateBatchPause = 50 * time.Millisecond

	// estimatedThroughputBytesPerSecond is the assumed transfer rate for
	// ETA calculations during workspace hydration.
	estimatedThroughputBytesPerSecond = 2 * 1024 * 1024 // 2 MB/s
)

// hydrateFileInfo collects metadata about a single file during the scan
// phase of cold hydration.
type hydrateFileInfo struct {
	path    string // relative to workspace root
	size    int64
	modTime time.Time
}

// isExcludedDir reports whether any component of the given path is in
// the excluded directory name list. Accepts relative paths.
func isExcludedDir(relPath string) bool {
	for dir := range excludedDirNames {
		if strings.HasPrefix(relPath, dir+"/") || relPath == dir {
			return true
		}
	}
	return false
}

// isHydrateBinaryFile reports whether the file extension indicates a binary
// file that should be skipped if it exceeds maxBinaryFileSize during
// workspace hydration. Uses extension-based detection (not content-based).
func isHydrateBinaryFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return excludedBinaryExtensions[ext]
}

// isSensitiveFile reports whether the given path refers to a file that
// may contain secrets or credentials and should be excluded from hydration.
// Checks both the base filename against a known list and the extension
// against a set of secret-related file types.
func isSensitiveFile(relPath string) bool {
	base := filepath.Base(relPath)
	if sensitiveFileNames[base] {
		return true
	}
	// Catch any .env.* variant (e.g., .env.production.local)
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	if sensitiveFileExts[strings.ToLower(filepath.Ext(relPath))] {
		return true
	}
	return false
}

// handleColdHydrateRequest scans the workspace root and streams all
// eligible files over the WebSocket connection so the browser can
// populate its OPFS replica (SP-046 §6).
//
// Protocol phases:
//  1. Scan workspace, collect file list
//  2. Send hydrate_manifest with totals and ETA
//  3. Stream each file as hydrate_file
//  4. Send hydrate_complete with transfer statistics
func (ws *ReactWebServer) handleColdHydrateRequest(safeConn *SafeConn, workspaceRoot string) {
	// Validate workspace root
	if workspaceRoot == "" {
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "workspace root is not configured"},
		})
		return
	}

	// Verify workspace exists
	info, err := os.Stat(workspaceRoot)
	if err != nil {
		errMsg := fmt.Sprintf("workspace root does not exist: %v", err)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": errMsg},
		})
		return
	}
	if !info.IsDir() {
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "workspace root is not a directory"},
		})
		return
	}

	startTime := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Phase 1: Scan workspace ---
	var files []hydrateFileInfo
	var totalSize int64

	err = filepath.Walk(workspaceRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			// Skip files we can't stat (permission errors, broken symlinks)
			return nil
		}

		// Skip directories
		if fi.IsDir() {
			rel, relErr := filepath.Rel(workspaceRoot, path)
			if relErr == nil && isExcludedDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks to prevent path traversal attacks
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Get relative path
		rel, relErr := filepath.Rel(workspaceRoot, path)
		if relErr != nil {
			return nil
		}

		// Skip excluded directories (belt and suspenders)
		if isExcludedDir(rel) {
			return nil
		}

		// Skip files that are too large
		fileSize := fi.Size()
		if fileSize > maxFileSize {
			ws.log().Debug("skipping oversized hydration file", slog.String("path", rel), slog.Int64("size_bytes", fileSize))
			return nil
		}

		// Skip large binary files (>1MB)
		if isHydrateBinaryFile(rel) && fileSize > maxBinaryFileSize {
			ws.log().Debug("skipping large binary hydration file", slog.String("path", rel), slog.Int64("size_bytes", fileSize))
			return nil
		}

		// Skip sensitive files that may contain secrets
		if isSensitiveFile(rel) {
			return nil
		}

		files = append(files, hydrateFileInfo{
			path:    rel,
			size:    fileSize,
			modTime: fi.ModTime(),
		})
		totalSize += fileSize

		return nil
	})

	if err != nil {
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("workspace scan failed: %v", err)},
		})
		return
	}

	// Sort files for deterministic ordering across scans
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	totalFiles := int64(len(files))

	// --- Phase 2: Send manifest ---
	estimateSeconds := int64(0)
	if totalSize > 0 {
		estimateSeconds = int64(totalSize) / estimatedThroughputBytesPerSecond
		if estimateSeconds < 1 {
			estimateSeconds = 1
		}
	}

	err = safeConn.WriteJSON(map[string]interface{}{
		"type": "hydrate_manifest",
		"data": HydrateManifestData{
			TotalFiles:      totalFiles,
			TotalSize:       totalSize,
			EstimateSeconds: estimateSeconds,
		},
	})
	if err != nil {
		ws.log().Error("hydration manifest send failed", slog.Any("err", err))
		return
	}

	ws.log().Info("workspace hydration streaming started", slog.Int64("file_count", totalFiles), slog.Int64("total_bytes", totalSize), slog.Int64("estimated_seconds", estimateSeconds), slog.String("workspace_root", workspaceRoot))

	// --- Phase 3: Stream files ---
	filesSent := int64(0)
	var bytesSent int64

	// Resolve the workspace root once for path validation (eval symlinks so
	// comparison with EvalSymlinks'd file paths is consistent — macOS /var
	// → /private/var, etc.).
	absRoot, rootErr := filepath.Abs(workspaceRoot)
	if rootErr != nil {
		ws.log().Warn("hydration workspace root resolution failed; using unresolved path", slog.Any("err", rootErr))
		absRoot = workspaceRoot
	}
	if evaled, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = evaled
	}

	for i, fi := range files {
		select {
		case <-ctx.Done():
			ws.log().Info("workspace hydration cancelled", slog.Int64("files_sent", filesSent), slog.Int64("total_files", totalFiles))
			return
		default:
		}

		// Resolve full path and validate it stays within workspace root
		fullPath := filepath.Join(workspaceRoot, fi.path)
		resolvedPath, evalErr := filepath.EvalSymlinks(fullPath)
		if evalErr != nil {
			ws.log().Warn("skipping hydration file with unresolvable path", slog.String("path", fi.path), slog.Any("err", evalErr))
			continue
		}
		if !strings.HasPrefix(resolvedPath, absRoot+string(filepath.Separator)) && resolvedPath != absRoot {
			ws.log().Warn("skipping hydration file outside workspace root", slog.String("path", fi.path))
			continue
		}

		// Stream file through base64 encoder to avoid double memory allocation
		var buf bytes.Buffer
		f, openErr := os.Open(fullPath)
		if openErr != nil {
			ws.log().Error("hydration file open failed", slog.String("path", fi.path), slog.Any("err", openErr))
			continue
		}
		encoder := base64.NewEncoder(base64.StdEncoding, &buf)
		if _, err := io.Copy(encoder, f); err != nil {
			f.Close()
			ws.log().Error("hydration file read failed", slog.String("path", fi.path), slog.Any("err", err))
			continue
		}
		encoder.Close()
		f.Close()
		contentB64 := buf.String()

		// Calculate progress
		progressPct := float64(i+1) / float64(totalFiles) * 100.0

		// Send file message
		err = safeConn.WriteJSON(map[string]interface{}{
			"type": "hydrate_file",
			"data": HydrateFileData{
				Path:          fi.path,
				ContentBase64: contentB64,
				Size:          fi.size,
				ModifiedAt:    fi.modTime.UTC().Format(time.RFC3339),
				ProgressPct:   progressPct,
			},
		})
		if err != nil {
			ws.log().Error("hydration file send failed", slog.String("path", fi.path), slog.Any("err", err))
			return
		}

		filesSent++
		bytesSent += fi.size

		// Batch pause every hydrateFileBatchSize files (context-aware)
		if (i+1)%hydrateFileBatchSize == 0 {
			select {
			case <-ctx.Done():
				ws.log().Info("workspace hydration cancelled during batch pause", slog.Int64("files_sent", filesSent))
				return
			case <-time.After(hydrateBatchPause):
			}
		}
	}

	// --- Phase 4: Send completion ---
	durationMs := time.Since(startTime).Milliseconds()

	err = safeConn.WriteJSON(map[string]interface{}{
		"type": "hydrate_complete",
		"data": HydrateCompleteData{
			FilesTransferred: filesSent,
			TotalBytes:       bytesSent,
			DurationMs:       durationMs,
		},
	})
	if err != nil {
		ws.log().Error("hydration completion send failed", slog.Any("err", err))
		return
	}

	ws.log().Info("workspace hydration completed", slog.Int64("files_sent", filesSent), slog.Int64("bytes_sent", bytesSent), slog.Int64("duration_ms", durationMs))
}
