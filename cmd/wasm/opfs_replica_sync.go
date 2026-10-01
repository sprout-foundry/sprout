//go:build js && wasm

package main

// opfs_replica_sync.go — the OPFS-replica sync half: syncOPFSReplica
// (push local files to the browser OPFS store), getOPFSFile, and
// storeReplicaMetadata. Split out of opfs_replica.go.
import (
	"encoding/base64"
	"encoding/json"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

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
