//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ─── Helper assertions ─────────────────────────────────────────────────

// expectMap asserts that result is a map[string]interface{} and returns it.
func expectMap(t *testing.T, result interface{}, label string) map[string]interface{} {
	t.Helper()
	resMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("%s is not a map[string]interface{}", label)
	}
	return resMap
}

// expectField checks a field in the result map. Handles int64 vs float64
// from JSON/JS bridge marshalling.
func expectField(t *testing.T, m map[string]interface{}, key string, expected interface{}) {
	t.Helper()
	val, ok := m[key]
	if !ok {
		t.Fatalf("%s: field %q not found", m, key)
	}
	switch e := expected.(type) {
	case int64:
		switch v := val.(type) {
		case float64:
			if int64(v) != e {
				t.Errorf("field %q = %v (%T), want %d", key, val, val, e)
			}
		case int64:
			if v != e {
				t.Errorf("field %q = %v, want %d", key, val, e)
			}
		default:
			t.Errorf("field %q type %T cannot compare with int64", key, val)
		}
	default:
		if val != expected {
			t.Errorf("field %q = %v (%T), want %v (%T)", key, val, val, expected, expected)
		}
	}
}

// expectError checks that the result map contains an "error" field
// containing the given substring.
func expectError(t *testing.T, m map[string]interface{}, substring string) {
	t.Helper()
	errVal, ok := m["error"]
	if !ok {
		t.Fatalf("expected error, got: %v", m)
	}
	errStr, ok := errVal.(string)
	if !ok {
		t.Fatalf("error field is not a string: %v (%T)", errVal, errVal)
	}
	if !strings.Contains(errStr, substring) {
		t.Errorf("error %q does not contain %q", errStr, substring)
	}
}

// ─── initOPFSReplicaFunc ─────────────────────────────────────────────

func TestInitOPFSReplicaFunc_EmptyManifest(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	args := []js.Value{js.ValueOf("[]")}
	result := initOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "init result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "fileCount", 0)
	expectField(t, resMap, "totalSize", int64(0))

	opfsReplicaMu.Lock()
	if len(opfsReplicaFiles) != 0 {
		t.Errorf("replica has %d entries, want 0", len(opfsReplicaFiles))
	}
	opfsReplicaMu.Unlock()
}

func TestInitOPFSReplicaFunc_MultipleFiles(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	manifest := []manifestEntry{
		{
			Path:                  "src/a.go",
			Size:                  100,
			WorkspaceFileMetadata: agent.WorkspaceFileMetadata{BrowserSeq: 1},
		},
		{
			Path:                  "src/b.go",
			Size:                  200,
			WorkspaceFileMetadata: agent.WorkspaceFileMetadata{BrowserSeq: 2},
		},
	}
	data, _ := json.Marshal(manifest)

	args := []js.Value{js.ValueOf(string(data))}
	result := initOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "init result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "fileCount", 2)
	expectField(t, resMap, "totalSize", int64(300))

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if len(opfsReplicaFiles) != 2 {
		t.Fatalf("replica has %d entries, want 2", len(opfsReplicaFiles))
	}
	entryA, ok := opfsReplicaFiles["src/a.go"]
	if !ok {
		t.Fatal("src/a.go not found in replica")
	}
	if entryA.Size != 100 {
		t.Errorf("src/a.go size = %d, want 100", entryA.Size)
	}
	if entryA.Metadata.BrowserSeq != 1 {
		t.Errorf("src/a.go BrowserSeq = %d, want 1", entryA.Metadata.BrowserSeq)
	}
	entryB, ok := opfsReplicaFiles["src/b.go"]
	if !ok {
		t.Fatal("src/b.go not found in replica")
	}
	if entryB.Size != 200 {
		t.Errorf("src/b.go size = %d, want 200", entryB.Size)
	}
}

func TestInitOPFSReplicaFunc_ReplacesExistingState(t *testing.T) {
	// Pre-populate with one file.
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"old.txt": {Path: "old.txt", Size: 50},
	}
	opfsReplicaMu.Unlock()

	// Init with new manifest — old files should be gone.
	manifest := []manifestEntry{{Path: "new.txt", Size: 10}}
	data, _ := json.Marshal(manifest)

	args := []js.Value{js.ValueOf(string(data))}
	initOPFSReplicaFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if _, ok := opfsReplicaFiles["old.txt"]; ok {
		t.Error("old.txt should have been replaced")
	}
	if _, ok := opfsReplicaFiles["new.txt"]; !ok {
		t.Error("new.txt should exist after init")
	}
}

func TestInitOPFSReplicaFunc_MissingArg(t *testing.T) {
	result := initOPFSReplicaFunc(js.Null(), []js.Value{})
	resMap := expectMap(t, result, "init result")
	expectError(t, resMap, "missing manifest argument")
}

func TestInitOPFSReplicaFunc_EmptyArg(t *testing.T) {
	args := []js.Value{js.ValueOf("")}
	result := initOPFSReplicaFunc(js.Null(), args)
	resMap := expectMap(t, result, "init result")
	expectError(t, resMap, "empty manifest")
}

func TestInitOPFSReplicaFunc_InvalidJSON(t *testing.T) {
	args := []js.Value{js.ValueOf("not json{")}
	result := initOPFSReplicaFunc(js.Null(), args)
	resMap := expectMap(t, result, "init result")
	expectError(t, resMap, "invalid manifest JSON")
}

func TestInitOPFSReplicaFunc_SetsLastSync(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	args := []js.Value{js.ValueOf("[{\"path\":\"x.txt\",\"size\":10}]")}
	initOPFSReplicaFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if opfsReplicaLastSync.IsZero() {
		t.Error("opfsReplicaLastSync should be set after init")
	}
}

// ─── getOPFSReplicaStatusFunc ────────────────────────────────────────

func TestGetOPFSReplicaStatusFunc_Empty(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	result := getOPFSReplicaStatusFunc(js.Null(), []js.Value{})
	resMap := expectMap(t, result, "status result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "fileCount", 0)
	expectField(t, resMap, "totalSize", int64(0))
	if lastSync, _ := resMap["lastSyncTimestamp"].(string); lastSync != "" {
		t.Errorf("lastSyncTimestamp = %q, want empty string", lastSync)
	}
}

func TestGetOPFSReplicaStatusFunc_WithData(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"foo.txt": {Path: "foo.txt", Size: 50},
		"bar.txt": {Path: "bar.txt", Size: 30},
	}
	opfsReplicaLastSync = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	opfsReplicaMu.Unlock()

	result := getOPFSReplicaStatusFunc(js.Null(), []js.Value{})
	resMap := expectMap(t, result, "status result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "fileCount", 2)
	expectField(t, resMap, "totalSize", int64(80))
	if lastSync, _ := resMap["lastSyncTimestamp"].(string); lastSync == "" {
		t.Error("lastSyncTimestamp should not be empty when last sync is set")
	}
}

// ─── syncOPFSReplicaFunc: upsert ─────────────────────────────────────

func TestSyncOPFSReplicaUpsert_WithMetadata(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	patch := patchEvent{
		Op:   "upsert",
		Path: "src/app.go",
		Metadata: &agent.WorkspaceFileMetadata{
			BrowserSeq:        5,
			ContainerSeq:      3,
			LastSyncedBrowser: 4,
		},
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry, ok := opfsReplicaFiles["src/app.go"]
	if !ok {
		t.Fatal("src/app.go not found in replica")
	}
	if entry.Metadata.BrowserSeq != 5 {
		t.Errorf("BrowserSeq = %d, want 5", entry.Metadata.BrowserSeq)
	}
	if entry.Metadata.ContainerSeq != 3 {
		t.Errorf("ContainerSeq = %d, want 3", entry.Metadata.ContainerSeq)
	}
	if entry.Metadata.LastSyncedBrowser != 4 {
		t.Errorf("LastSyncedBrowser = %d, want 4", entry.Metadata.LastSyncedBrowser)
	}
}

func TestSyncOPFSReplicaUpsert_WithContentBase64(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	originalContent := "package hello\n"
	contentB64 := base64.StdEncoding.EncodeToString([]byte(originalContent))

	patch := patchEvent{
		Op:            "upsert",
		Path:          "src/hello.go",
		ContentBase64: contentB64,
		Metadata:      &agent.WorkspaceFileMetadata{BrowserSeq: 1},
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry, ok := opfsReplicaFiles["src/hello.go"]
	if !ok {
		t.Fatal("src/hello.go not found")
	}
	if entry.Content != contentB64 {
		t.Errorf("Content = %q, want %q", entry.Content, contentB64)
	}
	if entry.Size != int64(len(originalContent)) {
		t.Errorf("Size = %d, want %d", entry.Size, len(originalContent))
	}
	if entry.Metadata.BrowserSeq != 1 {
		t.Errorf("BrowserSeq = %d, want 1", entry.Metadata.BrowserSeq)
	}
}

func TestSyncOPFSReplicaUpsert_NoMetadata(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	originalContent := "plain content"
	contentB64 := base64.StdEncoding.EncodeToString([]byte(originalContent))

	patch := patchEvent{
		Op:            "upsert",
		Path:          "src/plain.txt",
		ContentBase64: contentB64,
		// Metadata is nil
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry, ok := opfsReplicaFiles["src/plain.txt"]
	if !ok {
		t.Fatal("src/plain.txt not found")
	}
	if entry.Content != contentB64 {
		t.Errorf("Content = %q, want %q", entry.Content, contentB64)
	}
	if entry.Size != int64(len(originalContent)) {
		t.Errorf("Size = %d, want %d", entry.Size, len(originalContent))
	}
}

func TestSyncOPFSReplicaUpsert_UpdatesExisting(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/existing.go": {
			Path:     "src/existing.go",
			Size:     10,
			Content:  "bG9sZA==",
			Metadata: agent.WorkspaceFileMetadata{BrowserSeq: 1},
		},
	}
	opfsReplicaMu.Unlock()

	newContent := "updated content!!"
	contentB64 := base64.StdEncoding.EncodeToString([]byte(newContent))
	patch := patchEvent{
		Op:            "upsert",
		Path:          "src/existing.go",
		ContentBase64: contentB64,
		Metadata:      &agent.WorkspaceFileMetadata{BrowserSeq: 5},
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry := opfsReplicaFiles["src/existing.go"]
	if entry.Content != contentB64 {
		t.Errorf("Content = %q, want %q", entry.Content, contentB64)
	}
	if entry.Size != int64(len(newContent)) {
		t.Errorf("Size = %d, want %d", entry.Size, len(newContent))
	}
	if entry.Metadata.BrowserSeq != 5 {
		t.Errorf("BrowserSeq = %d, want 5", entry.Metadata.BrowserSeq)
	}
}

func TestSyncOPFSReplicaUpsert_MetadataOnly(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	patch := patchEvent{
		Op:       "upsert",
		Path:     "src/metaOnly.go",
		Metadata: &agent.WorkspaceFileMetadata{BrowserSeq: 7},
		// No ContentBase64
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry, ok := opfsReplicaFiles["src/metaOnly.go"]
	if !ok {
		t.Fatal("file not found")
	}
	if entry.Metadata.BrowserSeq != 7 {
		t.Errorf("BrowserSeq = %d, want 7", entry.Metadata.BrowserSeq)
	}
}

func TestSyncOPFSReplicaUpsert_InvalidBase64(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	patch := patchEvent{
		Op:            "upsert",
		Path:          "src/bad.txt",
		ContentBase64: "!!!not-base64!!!",
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectError(t, resMap, "invalid content_base64")
}

// ─── syncOPFSReplicaFunc: error cases ────────────────────────────────

func TestSyncOPFSReplicaFunc_MissingArg(t *testing.T) {
	result := syncOPFSReplicaFunc(js.Null(), []js.Value{})
	resMap := expectMap(t, result, "sync result")
	expectError(t, resMap, "missing patch argument")
}

func TestSyncOPFSReplicaFunc_EmptyPatch(t *testing.T) {
	args := []js.Value{js.ValueOf("")}
	result := syncOPFSReplicaFunc(js.Null(), args)
	resMap := expectMap(t, result, "sync result")
	expectError(t, resMap, "empty patch")
}

func TestSyncOPFSReplicaFunc_InvalidJSON(t *testing.T) {
	args := []js.Value{js.ValueOf("{bad json")}
	result := syncOPFSReplicaFunc(js.Null(), args)
	resMap := expectMap(t, result, "sync result")
	expectError(t, resMap, "invalid patch JSON")
}

// ─── syncOPFSReplicaFunc: delete ─────────────────────────────────────

func TestSyncOPFSReplicaDelete(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/toDelete.go": {Path: "src/toDelete.go", Size: 100},
		"src/keep.go":     {Path: "src/keep.go", Size: 50},
	}
	opfsReplicaMu.Unlock()

	patch := patchEvent{Op: "delete", Path: "src/toDelete.go"}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if _, ok := opfsReplicaFiles["src/toDelete.go"]; ok {
		t.Error("src/toDelete.go should be deleted")
	}
	if _, ok := opfsReplicaFiles["src/keep.go"]; !ok {
		t.Error("src/keep.go should still exist")
	}
}

func TestSyncOPFSReplicaDelete_NonExistent(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	patch := patchEvent{Op: "delete", Path: "src/missing.go"}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectField(t, resMap, "ok", true)
}

func TestSyncOPFSReplicaUnknownOp(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	patch := patchEvent{Op: "rename", Path: "src/x.go"}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	result := syncOPFSReplicaFunc(js.Null(), args)

	resMap := expectMap(t, result, "sync result")
	expectError(t, resMap, "unknown op")
}

// ─── getOPFSFileFunc ──────────────────────────────────────────────────

func TestGetOPFSFileFunc_Exists(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/hello.go": {
			Path:     "src/hello.go",
			Size:     25,
			Content:  "cGFja2FnZSBoZWxsbw==",
			Metadata: agent.WorkspaceFileMetadata{BrowserSeq: 5},
		},
	}
	opfsReplicaMu.Unlock()

	args := []js.Value{js.ValueOf("src/hello.go")}
	result := getOPFSFileFunc(js.Null(), args)

	resMap := expectMap(t, result, "get file result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "path", "src/hello.go")
	if exists, _ := resMap["exists"].(bool); !exists {
		t.Errorf("exists = %v, want true", exists)
	}
	meta, ok := resMap["metadata"].(agent.WorkspaceFileMetadata)
	if !ok {
		t.Fatal("metadata should be WorkspaceFileMetadata")
	}
	if meta.BrowserSeq != 5 {
		t.Errorf("metadata.BrowserSeq = %d, want 5", meta.BrowserSeq)
	}
}

func TestGetOPFSFileFunc_NotExists(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	args := []js.Value{js.ValueOf("src/missing.go")}
	result := getOPFSFileFunc(js.Null(), args)

	resMap := expectMap(t, result, "get file result")
	expectField(t, resMap, "ok", true)
	expectField(t, resMap, "path", "src/missing.go")
	if exists, _ := resMap["exists"].(bool); exists {
		t.Errorf("exists = %v, want false", exists)
	}
	if resMap["metadata"] != nil {
		t.Error("metadata should be nil for non-existent file")
	}
}

func TestGetOPFSFileFunc_MissingPath(t *testing.T) {
	result := getOPFSFileFunc(js.Null(), []js.Value{})
	resMap := expectMap(t, result, "get file result")
	expectError(t, resMap, "missing path argument")
}

func TestGetOPFSFileFunc_EmptyPath(t *testing.T) {
	args := []js.Value{js.ValueOf("")}
	result := getOPFSFileFunc(js.Null(), args)
	resMap := expectMap(t, result, "get file result")
	expectError(t, resMap, "missing path argument")
}

// ─── storeReplicaMetadataFunc ─────────────────────────────────────────

func TestStoreReplicaMetadata_PartialUpdate(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/app.go": {
			Path:    "src/app.go",
			Size:    100,
			Content: "b3JpZ2luYWw=",
			Metadata: agent.WorkspaceFileMetadata{
				BrowserSeq:        3,
				ContainerSeq:      7,
				LastSyncedBrowser: 2,
			},
		},
	}
	opfsReplicaMu.Unlock()

	// Update only BrowserSeq — other fields should be preserved (merge).
	metaData := agent.WorkspaceFileMetadata{BrowserSeq: 4}
	metaJSON, _ := json.Marshal(metaData)

	args := []js.Value{js.ValueOf("src/app.go"), js.ValueOf(string(metaJSON))}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry := opfsReplicaFiles["src/app.go"]
	if entry.Metadata.BrowserSeq != 4 {
		t.Errorf("BrowserSeq = %d, want 4", entry.Metadata.BrowserSeq)
	}
	if entry.Metadata.ContainerSeq != 7 {
		t.Errorf("ContainerSeq = %d, want 7 (should be preserved)", entry.Metadata.ContainerSeq)
	}
	if entry.Metadata.LastSyncedBrowser != 2 {
		t.Errorf("LastSyncedBrowser = %d, want 2 (should be preserved)", entry.Metadata.LastSyncedBrowser)
	}
	if entry.Content != "b3JpZ2luYWw=" {
		t.Errorf("Content was modified: got %q", entry.Content)
	}
}

func TestStoreReplicaMetadata_FullUpdate(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/app.go": {Path: "src/app.go", Size: 100, Content: "b3JpZ2luYWw="},
	}
	opfsReplicaMu.Unlock()

	metaData := agent.WorkspaceFileMetadata{
		BrowserSeq:          10,
		ContainerSeq:        20,
		LastSyncedBrowser:   9,
		LastSyncedContainer: 19,
		ModifiedAt:          time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	metaJSON, _ := json.Marshal(metaData)

	args := []js.Value{js.ValueOf("src/app.go"), js.ValueOf(string(metaJSON))}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry := opfsReplicaFiles["src/app.go"]
	if entry.Metadata.BrowserSeq != 10 {
		t.Errorf("BrowserSeq = %d, want 10", entry.Metadata.BrowserSeq)
	}
	if entry.Metadata.ContainerSeq != 20 {
		t.Errorf("ContainerSeq = %d, want 20", entry.Metadata.ContainerSeq)
	}
	if entry.Metadata.LastSyncedBrowser != 9 {
		t.Errorf("LastSyncedBrowser = %d, want 9", entry.Metadata.LastSyncedBrowser)
	}
	if entry.Metadata.LastSyncedContainer != 19 {
		t.Errorf("LastSyncedContainer = %d, want 19", entry.Metadata.LastSyncedContainer)
	}
}

func TestStoreReplicaMetadata_MissingPath(t *testing.T) {
	metaJSON, _ := json.Marshal(agent.WorkspaceFileMetadata{})
	args := []js.Value{js.Null(), js.ValueOf(string(metaJSON))}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectError(t, resMap, "missing path argument")
}

func TestStoreReplicaMetadata_MissingMetadataArg(t *testing.T) {
	args := []js.Value{js.ValueOf("src/x.go")}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectError(t, resMap, "missing metadata argument")
}

func TestStoreReplicaMetadata_EmptyMetadata(t *testing.T) {
	args := []js.Value{js.ValueOf("src/x.go"), js.ValueOf("")}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectError(t, resMap, "empty metadata")
}

func TestStoreReplicaMetadata_InvalidJSON(t *testing.T) {
	args := []js.Value{js.ValueOf("src/x.go"), js.ValueOf("{bad")}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectError(t, resMap, "invalid metadata JSON")
}

func TestStoreReplicaMetadata_NewFile(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaMu.Unlock()

	metaData := agent.WorkspaceFileMetadata{BrowserSeq: 1}
	metaJSON, _ := json.Marshal(metaData)

	args := []js.Value{js.ValueOf("src/new.go"), js.ValueOf(string(metaJSON))}
	result := storeReplicaMetadataFunc(js.Null(), args)

	resMap := expectMap(t, result, "store metadata result")
	expectField(t, resMap, "ok", true)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry, ok := opfsReplicaFiles["src/new.go"]
	if !ok {
		t.Fatal("new file entry should have been created")
	}
	if entry.Path != "src/new.go" {
		t.Errorf("Path = %q, want %q", entry.Path, "src/new.go")
	}
	if entry.Metadata.BrowserSeq != 1 {
		t.Errorf("BrowserSeq = %d, want 1", entry.Metadata.BrowserSeq)
	}
}

func TestStoreReplicaMetadata_ZeroValuesNotOverwritten(t *testing.T) {
	// The merge logic only updates non-zero fields.
	// Sending a zero BrowserSeq should NOT overwrite an existing value.
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{
		"src/x.go": {
			Path: "src/x.go",
			Metadata: agent.WorkspaceFileMetadata{
				BrowserSeq:   10,
				ContainerSeq: 8,
			},
		},
	}
	opfsReplicaMu.Unlock()

	// Send ContainerSeq=5 only. BrowserSeq=0 should be ignored.
	metaData := agent.WorkspaceFileMetadata{BrowserSeq: 0, ContainerSeq: 5}
	metaJSON, _ := json.Marshal(metaData)

	args := []js.Value{js.ValueOf("src/x.go"), js.ValueOf(string(metaJSON))}
	storeReplicaMetadataFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	entry := opfsReplicaFiles["src/x.go"]
	if entry.Metadata.BrowserSeq != 10 {
		t.Errorf("BrowserSeq = %d, want 10 (should not be overwritten by zero)", entry.Metadata.BrowserSeq)
	}
	if entry.Metadata.ContainerSeq != 5 {
		t.Errorf("ContainerSeq = %d, want 5", entry.Metadata.ContainerSeq)
	}
}

func TestStoreReplicaMetadata_UpdatesLastSync(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{"x.txt": {Path: "x.txt"}}
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	metaJSON, _ := json.Marshal(agent.WorkspaceFileMetadata{BrowserSeq: 1})
	args := []js.Value{js.ValueOf("x.txt"), js.ValueOf(string(metaJSON))}
	storeReplicaMetadataFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if opfsReplicaLastSync.IsZero() {
		t.Error("opfsReplicaLastSync should be set after store metadata")
	}
}

// ─── opfsReplicaJSFuncs registration ─────────────────────────────────

func TestOpfsReplicaJSFuncs_RegistersAll(t *testing.T) {
	funcs := opfsReplicaJSFuncs()
	expected := []string{
		"initOPFSReplica",
		"getOPFSReplicaStatus",
		"syncOPFSReplica",
		"getOPFSFile",
		"storeReplicaMetadata",
	}
	for _, name := range expected {
		if _, ok := funcs[name]; !ok {
			t.Errorf("opfsReplicaJSFuncs() must register %q", name)
		}
	}
}

// ─── lastSync updated on sync operations ──────────────────────────────

func TestSyncOPFSReplicaUpsert_UpdatesLastSync(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	patch := patchEvent{
		Op: "upsert", Path: "src/x.go",
		Metadata: &agent.WorkspaceFileMetadata{BrowserSeq: 1},
	}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	syncOPFSReplicaFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if opfsReplicaLastSync.IsZero() {
		t.Error("opfsReplicaLastSync should be set after upsert")
	}
}

func TestDeleteUpdatesLastSync(t *testing.T) {
	opfsReplicaMu.Lock()
	opfsReplicaFiles = map[string]*replicaFileEntry{"x.txt": {Path: "x.txt"}}
	opfsReplicaLastSync = time.Time{}
	opfsReplicaMu.Unlock()

	patch := patchEvent{Op: "delete", Path: "x.txt"}
	data, _ := json.Marshal(patch)
	args := []js.Value{js.ValueOf(string(data))}
	syncOPFSReplicaFunc(js.Null(), args)

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()
	if opfsReplicaLastSync.IsZero() {
		t.Error("opfsReplicaLastSync should be set after delete")
	}
}
