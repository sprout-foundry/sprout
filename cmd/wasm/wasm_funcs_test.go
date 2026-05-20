//go:build js && wasm

// Tests for the WASM JS bridge helpers. Run via:
//
//	GOOS=js GOARCH=wasm go test \
//	  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  ./cmd/wasm/
//
// Most of the WASM bridge surface depends on js.Value and a live JS host,
// which is hard to fake. These tests pin only the pure-Go logic that
// matters for correctness: memory-name sanitization (security), record→JS
// shaping (UI contract), and the linear-scan helpers.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

func TestSaveMemoryToDisk_RejectsPathTraversal(t *testing.T) {
	cases := []string{"../escape", "..", ".", "", "foo/bar", "foo\\bar", "../../etc/passwd"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			err := saveMemoryToDisk(name, "content")
			if err == nil {
				t.Errorf("saveMemoryToDisk(%q) should have rejected the name", name)
				return
			}
			if !strings.Contains(err.Error(), "invalid memory name") &&
				!strings.Contains(err.Error(), "memory directory unavailable") {
				t.Errorf("error for %q should be sanitization-flavored, got %v", name, err)
			}
		})
	}
}

func TestDeleteMemoryFromDisk_RejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "..", ".", "", "foo/bar"} {
		t.Run(name, func(t *testing.T) {
			err := deleteMemoryFromDisk(name)
			if err == nil {
				t.Errorf("deleteMemoryFromDisk(%q) should have rejected the name", name)
			}
		})
	}
}

func TestIndexOfID(t *testing.T) {
	records := []embedding.VectorRecord{
		{ID: "alpha"},
		{ID: "beta"},
		{ID: "gamma"},
	}
	cases := []struct {
		id   string
		want int
	}{
		{"alpha", 0},
		{"beta", 1},
		{"gamma", 2},
		{"missing", -1},
		{"", -1},
	}
	for _, c := range cases {
		got := indexOfID(records, c.id)
		if got != c.want {
			t.Errorf("indexOfID(%q) = %d, want %d", c.id, got, c.want)
		}
	}
}

func TestTurnRecordToJS_StripsEmbeddingAndPropagatesMetadata(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	rec := embedding.VectorRecord{
		ID:        "turn-1",
		Signature: "hello world",
		IndexedAt: now,
		Type:      "conversation_turn",
		Embedding: []float32{0.1, 0.2, 0.3}, // should NOT make it into the JS payload
		Metadata: map[string]interface{}{
			"sessionId":         "sess-abc",
			"turnNumber":        3,
			"workingDir":        "/home/user/proj",
			"duration":          1.5,
			"tokenUsage":        450,
			"actionableSummary": "Said hi",
			"filesTouched":      []string{"main.go"},
		},
	}
	got := turnRecordToJS(rec)

	// Required fields
	if got["id"] != "turn-1" {
		t.Errorf("id = %v", got["id"])
	}
	if got["userPrompt"] != "hello world" {
		t.Errorf("userPrompt = %v", got["userPrompt"])
	}
	if got["indexedAt"] != now.Format(time.RFC3339Nano) {
		t.Errorf("indexedAt = %v", got["indexedAt"])
	}

	// Metadata propagation
	if got["sessionId"] != "sess-abc" {
		t.Errorf("sessionId = %v", got["sessionId"])
	}
	if got["turnNumber"] != 3 {
		t.Errorf("turnNumber = %v", got["turnNumber"])
	}
	if got["workingDir"] != "/home/user/proj" {
		t.Errorf("workingDir = %v", got["workingDir"])
	}
	if got["actionableSummary"] != "Said hi" {
		t.Errorf("actionableSummary = %v", got["actionableSummary"])
	}

	// Embedding must not leak — it's large and useless to the browser side.
	if _, present := got["embedding"]; present {
		t.Error("turnRecordToJS leaked the embedding vector into the JS payload")
	}

	// Deleted flag absent by default; only present when explicitly set
	if _, present := got["deleted"]; present {
		t.Error("deleted flag should be absent when metadata.deleted is unset")
	}
}

func TestTurnRecordToJS_DeletedFlagPropagates(t *testing.T) {
	rec := embedding.VectorRecord{
		ID:        "turn-x",
		Signature: "deleted thing",
		Metadata: map[string]interface{}{
			"sessionId": "sess",
			"deleted":   true,
		},
	}
	got := turnRecordToJS(rec)
	if got["deleted"] != true {
		t.Errorf("deleted should be true, got %v", got["deleted"])
	}
}

func TestTurnRecordToJS_NilMetadataIsSafe(t *testing.T) {
	rec := embedding.VectorRecord{ID: "turn-y", Signature: "no metadata"}
	got := turnRecordToJS(rec)
	if got["id"] != "turn-y" {
		t.Errorf("id = %v", got["id"])
	}
	// Should not panic on nil Metadata; should also not invent fields.
	for _, key := range []string{"sessionId", "workingDir", "duration", "tokenUsage"} {
		if _, present := got[key]; present {
			t.Errorf("unexpected key %q present with nil metadata", key)
		}
	}
}
