//go:build js && wasm

package main

// opfs_replica_hydrate.go — the OPFS-replica hydrate half: the
// processHydrate{Manifest,File,Complete} browser-push handlers,
// getHydrateProgress, and the shared hydrateProgress state.
// Split out of opfs_replica.go.
import (
	"encoding/base64"
	"encoding/json"
	"syscall/js"
	"time"
)

// processHydrateManifestFunc receives the manifest from a cold-hydrate stream
// and initialises the Go-side progress tracker.
//
// Signature: processHydrateManifest(manifestJSON: string): {ok, total_files, total_size}
func processHydrateManifestFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing manifest argument"}
	}
	manifestJSON := args[0].String()
	if manifestJSON == "" {
		return map[string]interface{}{"error": "empty manifest"}
	}

	var data struct {
		TotalFiles      int64 `json:"total_files"`
		TotalSize       int64 `json:"total_size"`
		EstimateSeconds int64 `json:"estimate_seconds"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &data); err != nil {
		return map[string]interface{}{"error": "invalid manifest JSON: " + err.Error()}
	}

	hydrateProgressMu.Lock()
	hydrateProgress = HydrateProgressState{
		TotalFiles:       data.TotalFiles,
		TotalSize:        data.TotalSize,
		FilesReceived:    0,
		BytesReceived:    0,
		EstimatedSeconds: data.EstimateSeconds,
		Completed:        false,
		StartTime:        time.Now(),
	}
	hydrateProgressMu.Unlock()

	return map[string]interface{}{
		"ok":          true,
		"total_files": data.TotalFiles,
		"total_size":  data.TotalSize,
	}
}

// ─── processHydrateFile ─────────────────────────────────────────────────────

// processHydrateFileFunc receives a single file from the hydration stream,
// decodes its content, and stores it in the replica map.
//
// Signature: processHydrateFile(fileJSON: string): {ok, path, progress_pct}
func processHydrateFileFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing file argument"}
	}
	fileJSON := args[0].String()
	if fileJSON == "" {
		return map[string]interface{}{"error": "empty file payload"}
	}

	var data struct {
		Path          string  `json:"path"`
		ContentBase64 string  `json:"content_base64"`
		Size          int64   `json:"size"`
		ModifiedAt    string  `json:"modified_at"`
		ProgressPct   float64 `json:"progress_pct"`
	}
	if err := json.Unmarshal([]byte(fileJSON), &data); err != nil {
		return map[string]interface{}{"error": "invalid file JSON: " + err.Error()}
	}

	// Decode content
	var content string
	if data.ContentBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(data.ContentBase64)
		if err != nil {
			return map[string]interface{}{"error": "invalid content_base64: " + err.Error()}
		}
		_ = decoded // content is stored in OPFS by the JS side via the return value
		content = data.ContentBase64
	}

	// Update replica entry
	opfsReplicaMu.Lock()
	entry, exists := opfsReplicaFiles[data.Path]
	if !exists {
		entry = &replicaFileEntry{Path: data.Path}
		opfsReplicaFiles[data.Path] = entry
	}
	entry.Size = data.Size
	entry.Content = content
	if data.ModifiedAt != "" {
		if t, err := time.Parse(time.RFC3339, data.ModifiedAt); err == nil {
			entry.Metadata.ModifiedAt = t
		}
	}
	opfsReplicaMu.Unlock()

	// Update progress
	hydrateProgressMu.Lock()
	hydrateProgress.FilesReceived++
	hydrateProgress.BytesReceived += data.Size
	hydrateProgressMu.Unlock()

	return map[string]interface{}{
		"ok":           true,
		"path":         data.Path,
		"progress_pct": data.ProgressPct,
	}
}

// ─── processHydrateComplete ─────────────────────────────────────────────────

// processHydrateCompleteFunc marks the hydration as complete and records
// final statistics.
//
// Signature: processHydrateComplete(completeJSON: string): {ok, files_transferred, total_bytes, duration_ms}
func processHydrateCompleteFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing complete argument"}
	}
	completeJSON := args[0].String()
	if completeJSON == "" {
		return map[string]interface{}{"error": "empty complete payload"}
	}

	var data struct {
		FilesTransferred int64 `json:"files_transferred"`
		TotalBytes       int64 `json:"total_bytes"`
		DurationMs       int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal([]byte(completeJSON), &data); err != nil {
		return map[string]interface{}{"error": "invalid complete JSON: " + err.Error()}
	}

	hydrateProgressMu.Lock()
	hydrateProgress.Completed = true
	hydrateProgressMu.Unlock()

	// Update replica sync time
	opfsReplicaMu.Lock()
	opfsReplicaLastSync = time.Now()
	opfsReplicaMu.Unlock()

	return map[string]interface{}{
		"ok":                true,
		"files_transferred": data.FilesTransferred,
		"total_bytes":       data.TotalBytes,
		"duration_ms":       data.DurationMs,
	}
}

// ─── getHydrateProgress ─────────────────────────────────────────────────────

// getHydrateProgressFunc returns the current hydration progress state.
//
// Signature: getHydrateProgress(): {ok, total_files, total_size, files_received, bytes_received, progress_pct, completed}
func getHydrateProgressFunc(_ js.Value, args []js.Value) interface{} {
	hydrateProgressMu.Lock()
	defer hydrateProgressMu.Unlock()

	progressPct := float64(0)
	if hydrateProgress.TotalFiles > 0 {
		progressPct = float64(hydrateProgress.FilesReceived) / float64(hydrateProgress.TotalFiles) * 100.0
	}

	return map[string]interface{}{
		"ok":                true,
		"total_files":       hydrateProgress.TotalFiles,
		"total_size":        hydrateProgress.TotalSize,
		"files_received":    hydrateProgress.FilesReceived,
		"bytes_received":    hydrateProgress.BytesReceived,
		"progress_pct":      progressPct,
		"completed":         hydrateProgress.Completed,
		"estimated_seconds": hydrateProgress.EstimatedSeconds,
	}
}

// ─── SP-046 Sync Recovery ───────────────────────────────────────────────
