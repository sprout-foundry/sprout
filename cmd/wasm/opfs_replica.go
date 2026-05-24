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

// ─── Registration ────────────────────────────────────────────────────────

// opfsReplicaJSFuncs returns the OPFS-replica JS bridge functions so
// sync_funcs.go can merge them into the shared export map.
func opfsReplicaJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"initOPFSReplica":      js.FuncOf(initOPFSReplicaFunc),
		"getOPFSReplicaStatus": js.FuncOf(getOPFSReplicaStatusFunc),
		"syncOPFSReplica":      js.FuncOf(syncOPFSReplicaFunc),
		"getOPFSFile":          js.FuncOf(getOPFSFileFunc),
		"storeReplicaMetadata": js.FuncOf(storeReplicaMetadataFunc),
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
