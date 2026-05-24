//go:build !js

package webui

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// excludedDirNames lists directory names that are skipped during workspace
// hydration scans. These are build artifacts, dependency caches, and VCS
// data that are not needed in the OPFS replica.
var excludedDirNames = map[string]bool{
	".git":        true,
	"node_modules": true,
	".DS_Store":   true,
	"__pycache__": true,
	".next":       true,
	"dist":        true,
	".cache":      true,
}

// excludedBinaryExtensions lists file extensions that are considered binary.
// These files are still transferred, but those larger than 1MB are skipped.
var excludedBinaryExtensions = map[string]bool{
	".exe":   true, ".dll":    true, ".so":     true, ".dylib": true,
	".png":   true, ".jpg":   true, ".jpeg":   true, ".gif":   true,
	".ico":   true, ".woff":  true, ".woff2":  true, ".ttf":   true,
	".eot":   true, ".zip":   true, ".tar":    true, ".gz":    true,
	".rar":   true, ".7z":    true, ".mp3":    true, ".mp4":   true,
	".avi":   true, ".mov":   true, ".wmv":    true, ".wasm":  true,
	".sqlite": true, ".db":   true,
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

// handleColdHydrateRequest scans the workspace root and streams all
// eligible files over the WebSocket connection so the browser can
// populate its OPFS replica (SP-046 §6).
//
// Protocol phases:
//   1. Scan workspace, collect file list
//   2. Send hydrate_manifest with totals and ETA
//   3. Stream each file as hydrate_file
//   4. Send hydrate_complete with transfer statistics
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
	ctx := context.Background()

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
			log.Printf("[hydrate] Skipping oversized file: %s (%d bytes)", rel, fileSize)
			return nil
		}

		// Skip large binary files (>1MB)
		if isHydrateBinaryFile(rel) && fileSize > maxBinaryFileSize {
			log.Printf("[hydrate] Skipping large binary file: %s (%d bytes)", rel, fileSize)
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

	totalFiles := int64(len(files))

	// --- Phase 2: Send manifest ---
	// Estimate ~2MB/s throughput for ETA calculation
	estimateSeconds := int64(0)
	if totalSize > 0 {
		estimateSeconds = totalSize / (2 * 1024 * 1024)
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
		log.Printf("[hydrate] Failed to send manifest: %v", err)
		return
	}

	log.Printf("[hydrate] Streaming %d files (%d bytes, ~%ds ETA) for workspace %s",
		totalFiles, totalSize, estimateSeconds, workspaceRoot)

	// --- Phase 3: Stream files ---
	filesSent := int64(0)
	var bytesSent int64

	for i, fi := range files {
		select {
		case <-ctx.Done():
			log.Printf("[hydrate] Context cancelled after %d/%d files", filesSent, totalFiles)
			return
		default:
		}

		// Read file content
		fullPath := filepath.Join(workspaceRoot, fi.path)
		content, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			log.Printf("[hydrate] Failed to read file %s: %v", fi.path, readErr)
			continue
		}

		// Base64 encode
		contentB64 := base64.StdEncoding.EncodeToString(content)

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
			log.Printf("[hydrate] Failed to send file %s: %v", fi.path, err)
			return
		}

		filesSent++
		bytesSent += fi.size

		// Batch pause every hydrateFileBatchSize files
		if (i+1)%hydrateFileBatchSize == 0 {
			time.Sleep(hydrateBatchPause)
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
		log.Printf("[hydrate] Failed to send completion: %v", err)
		return
	}

	log.Printf("[hydrate] Complete: %d files, %d bytes in %dms", filesSent, bytesSent, durationMs)
}
