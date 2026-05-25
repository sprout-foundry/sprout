//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ─── OPFS Replica State ─────────────────────────────────────────────────

// opfsReplica state lives in Go so the container-side agent can query it
// without hitting IndexedDB.  The browser pushes an initial manifest and
// then incremental patch events; the replica mirrors what the browser
// considers "current".

// replicaFileEntry wraps WorkspaceFileMetadata with the file path and
// optional base64-encoded content so the replica is a fully self-contained
// mirror of OPFS state.
type replicaFileEntry struct {
	Path     string                // file path (key in the map)
	Metadata agent.WorkspaceFileMetadata
	Size     int64                 // file size in bytes
	Content  string                // base64-encoded content (optional)
}

var (
	opfsReplicaMu       sync.Mutex
	opfsReplicaFiles    = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync time.Time
)

// HydrateProgressState tracks the progress of an in-flight cold hydration.
type HydrateProgressState struct {
	TotalFiles       int64     `json:"total_files"`
	TotalSize        int64     `json:"total_size"`
	FilesReceived    int64     `json:"files_received"`
	BytesReceived    int64     `json:"bytes_received"`
	EstimatedSeconds int64     `json:"estimated_seconds"`
	Completed        bool      `json:"completed"`
	StartTime        time.Time `json:"start_time,omitempty"`
}

var (
	hydrateProgress   HydrateProgressState
	hydrateProgressMu sync.Mutex
)

// ─── Registration ────────────────────────────────────────────────────────

// opfsReplicaJSFuncs returns the OPFS-replica JS bridge functions so
// sync_funcs.go can merge them into the shared export map.
func opfsReplicaJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"initOPFSReplica":        js.FuncOf(initOPFSReplicaFunc),
		"getOPFSReplicaStatus":   js.FuncOf(getOPFSReplicaStatusFunc),
		"syncOPFSReplica":        js.FuncOf(syncOPFSReplicaFunc),
		"getOPFSFile":            js.FuncOf(getOPFSFileFunc),
		"storeReplicaMetadata":   js.FuncOf(storeReplicaMetadataFunc),
		"processHydrateManifest": js.FuncOf(processHydrateManifestFunc),
		"processHydrateFile":     js.FuncOf(processHydrateFileFunc),
		"processHydrateComplete": js.FuncOf(processHydrateCompleteFunc),
		"getHydrateProgress":     js.FuncOf(getHydrateProgressFunc),

		// SP-046 sync recovery (server-side failure recovery paths)
		"initSyncRecovery":       js.FuncOf(initSyncRecoveryFunc),
		"handleSyncReconcile":    js.FuncOf(handleSyncReconcileFunc),
		"recoverFromBrowserCrash": js.FuncOf(recoverFromBrowserCrashFunc),
	}
}

// ─── initOPFSReplica ────────────────────────────────────────────────────

// manifestEntry represents a single entry in the browser-provided manifest.
// The browser serialises the path alongside WorkspaceFileMetadata because
// the struct itself carries no path.
type manifestEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size,omitempty"`
	agent.WorkspaceFileMetadata
}

// initOPFSReplicaFunc initialises the replica from a JSON manifest produced
// by the browser-side IndexedDB store.  The manifest is a JSON array of
// objects, each containing a "path" field and WorkspaceFileMetadata fields.
//
// Signature: initOPFSReplica(manifestJSON: string): {ok, fileCount, totalSize}
func initOPFSReplicaFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing manifest argument"}
	}
	manifestJSON := args[0].String()
	if manifestJSON == "" {
		return map[string]interface{}{"error": "empty manifest"}
	}

	var manifest []manifestEntry
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return map[string]interface{}{"error": "invalid manifest JSON: " + err.Error()}
	}

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	opfsReplicaFiles = make(map[string]*replicaFileEntry, len(manifest))
	var totalSize int64
	for i := range manifest {
		e := manifest[i]
		entry := &replicaFileEntry{
			Path:     e.Path,
			Metadata: e.WorkspaceFileMetadata,
			Size:     e.Size,
		}
		opfsReplicaFiles[e.Path] = entry
		totalSize += e.Size
	}
	opfsReplicaLastSync = time.Now()

	return map[string]interface{}{
		"ok":        true,
		"fileCount": len(manifest),
		"totalSize": totalSize,
	}
}

// ─── getOPFSReplicaStatus ───────────────────────────────────────────────

// getOPFSReplicaStatusFunc returns high-level replica statistics.
//
// Signature: getOPFSReplicaStatus(): {ok, fileCount, totalSize, lastSyncTimestamp}
func getOPFSReplicaStatusFunc(_ js.Value, args []js.Value) interface{} {
	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	var totalSize int64
	for _, e := range opfsReplicaFiles {
		totalSize += e.Size
	}

	return map[string]interface{}{
		"ok":                true,
		"fileCount":         len(opfsReplicaFiles),
		"totalSize":         totalSize,
		"lastSyncTimestamp": opfsReplicaLastSync.Format(time.RFC3339),
	}
}

// ─── syncOPFSReplica ────────────────────────────────────────────────────

// patchEvent represents a single workspace-patch event pushed by the browser.
type patchEvent struct {
	Op            string                       `json:"op"`
	Path          string                       `json:"path"`
	ContentBase64 string                       `json:"content_base64,omitempty"`
	Metadata      *agent.WorkspaceFileMetadata `json:"metadata,omitempty"`
}

// syncOPFSReplicaFunc applies a browser-sourced patch event to the Go-side
// replica state.  Supported ops: "upsert" (add/update) and "delete".
//
// Signature: syncOPFSReplica(patchJSON: string): {ok}
func syncOPFSReplicaFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing patch argument"}
	}
	patchJSON := args[0].String()
	if patchJSON == "" {
		return map[string]interface{}{"error": "empty patch"}
	}

	var patch patchEvent
	if err := json.Unmarshal([]byte(patchJSON), &patch); err != nil {
		return map[string]interface{}{"error": "invalid patch JSON: " + err.Error()}
	}

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	switch patch.Op {
	case "upsert":
		if patch.Metadata == nil {
			// No metadata — create a minimal entry from content alone.
			var size int64
			var content string
			if patch.ContentBase64 != "" {
				decoded, err := base64.StdEncoding.DecodeString(patch.ContentBase64)
				if err != nil {
					return map[string]interface{}{"error": "invalid content_base64: " + err.Error()}
				}
				size = int64(len(decoded))
				content = patch.ContentBase64
			}
			entry, exists := opfsReplicaFiles[patch.Path]
			if !exists {
				entry = &replicaFileEntry{
					Path: patch.Path,
				}
				opfsReplicaFiles[patch.Path] = entry
			}
			entry.Size = size
			entry.Content = content
		} else {
			m := *patch.Metadata // copy
			entry, exists := opfsReplicaFiles[patch.Path]
			if !exists {
				entry = &replicaFileEntry{Path: patch.Path}
				opfsReplicaFiles[patch.Path] = entry
			}
			entry.Metadata = m
			if patch.ContentBase64 != "" {
				decoded, err := base64.StdEncoding.DecodeString(patch.ContentBase64)
				if err != nil {
					return map[string]interface{}{"error": "invalid content_base64: " + err.Error()}
				}
				entry.Size = int64(len(decoded))
				entry.Content = patch.ContentBase64
			}
		}
		opfsReplicaLastSync = time.Now()

	case "delete":
		delete(opfsReplicaFiles, patch.Path)
		opfsReplicaLastSync = time.Now()

	default:
		return map[string]interface{}{"error": "unknown op: " + patch.Op}
	}

	return map[string]interface{}{"ok": true}
}

// ─── getOPFSFile ────────────────────────────────────────────────────────

// getOPFSFileFunc looks up a single file entry in the replica state.
//
// Signature: getOPFSFile(path: string): {ok, path, exists, metadata}
func getOPFSFileFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	if path == "" {
		return map[string]interface{}{"error": "missing path argument"}
	}

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	entry, exists := opfsReplicaFiles[path]
	if !exists {
		return map[string]interface{}{
			"ok":       true,
			"path":     path,
			"exists":   false,
			"metadata": nil,
		}
	}

	return map[string]interface{}{
		"ok":       true,
		"path":     path,
		"exists":   true,
		"metadata": entry.Metadata,
	}
}

// ─── storeReplicaMetadata ───────────────────────────────────────────────

// storeReplicaMetadataFunc stores or updates per-file metadata in the
// replica state.  If the path has no existing entry a new one is created.
//
// Signature: storeReplicaMetadata(path: string, metadataJSON: string): {ok}
func storeReplicaMetadataFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	if path == "" {
		return map[string]interface{}{"error": "missing path argument"}
	}
	if len(args) < 2 {
		return map[string]interface{}{"error": "missing metadata argument"}
	}
	metaJSON := args[1].String()
	if metaJSON == "" {
		return map[string]interface{}{"error": "empty metadata"}
	}

	var meta agent.WorkspaceFileMetadata
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return map[string]interface{}{"error": "invalid metadata JSON: " + err.Error()}
	}

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	entry, exists := opfsReplicaFiles[path]
	if !exists {
		entry = &replicaFileEntry{Path: path}
		opfsReplicaFiles[path] = entry
	}

	// Merge: update only non-zero fields so partial metadata updates
	// don't wipe existing values.
	if meta.BrowserSeq != 0 {
		entry.Metadata.BrowserSeq = meta.BrowserSeq
	}
	if meta.ContainerSeq != 0 {
		entry.Metadata.ContainerSeq = meta.ContainerSeq
	}
	if meta.LastSyncedBrowser != 0 {
		entry.Metadata.LastSyncedBrowser = meta.LastSyncedBrowser
	}
	if meta.LastSyncedContainer != 0 {
		entry.Metadata.LastSyncedContainer = meta.LastSyncedContainer
	}
	if !meta.ModifiedAt.IsZero() {
		entry.Metadata.ModifiedAt = meta.ModifiedAt
	}

	opfsReplicaLastSync = time.Now()

	return map[string]interface{}{"ok": true}
}

// ─── processHydrateManifest ─────────────────────────────────────────────────

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
		"total_files":  data.TotalFiles,
		"total_size":   data.TotalSize,
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
		"ok":          true,
		"path":        data.Path,
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
		"ok":               true,
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
		"ok":               true,
		"total_files":      hydrateProgress.TotalFiles,
		"total_size":       hydrateProgress.TotalSize,
		"files_received":   hydrateProgress.FilesReceived,
		"bytes_received":   hydrateProgress.BytesReceived,
		"progress_pct":     progressPct,
		"completed":        hydrateProgress.Completed,
		"estimated_seconds": hydrateProgress.EstimatedSeconds,
	}
}

// ─── SP-046 Sync Recovery ───────────────────────────────────────────────

// initSyncRecoveryFunc is called on WASM startup to check for persisted state
// and send a sync_recover message to the server if needed.
//
// Signature: initSyncRecovery(clientID, sendWebSocketFunc): void
func initSyncRecoveryFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		println("[SP-046] initSyncRecovery: expected (clientID, sendWebSocketFunc)")
		return nil
	}
	clientID := args[0].String()
	sendFunc := args[1]

	// Read persisted seq numbers from OPFS metadata
	seqs := getPersistedSeqNumbers()

	if len(seqs) == 0 {
		println("[SP-046] initSyncRecovery: no persisted state, skipping recovery")
		return nil
	}

	println("[SP-046] initSyncRecovery: found", len(seqs), "persisted files, sending sync_recover")

	// Build the sync_recover message
	seqsObj := js.Global().Get("Object").New()
	for path, seq := range seqs {
		seqsObj.Set(path, seq)
	}

	msg := js.Global().Get("Object").New()
	msg.Set("type", "sync_recover")
	data := js.Global().Get("Object").New()
	data.Set("client_id", clientID)
	data.Set("seqs", seqsObj)
	msg.Set("data", data)

	// Send via the provided callback
	sendFunc.Invoke(msg)

	return nil
}

// handleSyncReconcileFunc processes the server's reconciliation plan.
//
// Signature: handleSyncReconcile(planData): void
func handleSyncReconcileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		println("[SP-046] handleSyncReconcile: expected reconcile plan")
		return nil
	}

	planData := args[0]
	if !planData.Truthy() {
		println("[SP-046] handleSyncReconcile: nil plan data")
		return nil
	}

	// Extract the plan array
	planArr := planData.Get("plan")
	if !planArr.Truthy() {
		println("[SP-046] handleSyncReconcile: no plan array")
		return nil
	}

	length := planArr.Length()
	println("[SP-046] handleSyncReconcile: processing", length, "items")

	for i := 0; i < length; i++ {
		item := planArr.Index(i)
		action := item.Get("action").String()
		filePath := item.Get("file_path").String()

		switch action {
		case "sync_ok":
			// Nothing to do
			println("[SP-046] handleSyncReconcile:", filePath, "is in sync")
		case "container_ahead":
			// Will receive replay patches — handled by handleReplayFile
			println("[SP-046] handleSyncReconcile:", filePath, "needs replay from container")
		case "browser_ahead":
			// Browser has newer data — will push to server
			println("[SP-046] handleSyncReconcile:", filePath, "browser is ahead, will push")
		case "diverged":
			// Conflict — log it, let the UI handle
			println("[SP-046] handleSyncReconcile:", filePath, "DIVERGED - conflict resolution needed")
		default:
			println("[SP-046] handleSyncReconcile: unknown action", action, "for", filePath)
		}
	}

	return nil
}

// getPersistedSeqNumbers reads persisted sequence numbers from OPFS metadata.
func getPersistedSeqNumbers() map[string]int64 {
	// This reads from the OPFS file metadata store that was persisted
	// before the browser crash. The actual OPFS read is done via JS interop.
	seqs := make(map[string]int64)

	// Try to read from the global OPFS metadata store
	opfsMeta := js.Global().Get("opfsMetadata")
	if !opfsMeta.Truthy() {
		return seqs
	}

	keys := js.Global().Get("Object").Call("keys", opfsMeta)
	for i := 0; i < keys.Length(); i++ {
		key := keys.Index(i).String()
		val := opfsMeta.Get(key)
		if seq := val.Get("browserSeq"); seq.Truthy() {
			if seq.Type() == js.TypeNumber {
				seqs[key] = int64(seq.Int())
			}
		}
	}

	return seqs
}

// recoverFromBrowserCrashFunc reads persisted seq numbers from OPFS metadata
// and returns them for recovery processing.
//
// Signature: recoverFromBrowserCrash(): {path: seq, ...}
func recoverFromBrowserCrashFunc(this js.Value, args []js.Value) interface{} {
	seqs := getPersistedSeqNumbers()

	result := js.Global().Get("Object").New()
	for path, seq := range seqs {
		result.Set(path, seq)
	}
	return result
}
