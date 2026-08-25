// Package events — tests for shell approval event helpers (SP-093-3).
package events

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellApprovalRequestEvent_BasicFields(t *testing.T) {
	parts := []ShellApprovalPartArg{
		{ID: "part-0", Text: "echo hello", Kind: "builtin", Semantic: "print text", Risk: "Low"},
		{ID: "part-1", Text: "rm -rf /tmp/data", Kind: "rm", Semantic: "recursive delete", Risk: "High"},
	}
	ev := ShellApprovalRequestEvent("req-1", "echo hello && rm -rf /tmp/data", parts, "echo hello\nrm -rf /tmp/data", "High")

	assert.Equal(t, "req-1", ev["request_id"])
	assert.Equal(t, "echo hello && rm -rf /tmp/data", ev["command"])
	assert.Equal(t, "echo hello\nrm -rf /tmp/data", ev["unified_view"])
	assert.Equal(t, "High", ev["risk_level"])
	assert.NotEmpty(t, ev["timestamp"])

	// Parts are serialized as []ShellApprovalPart (structs), not as the arg types.
	partsSlice, ok := ev["parts"].([]ShellApprovalPart)
	require.True(t, ok, "parts should be []ShellApprovalPart")
	assert.Len(t, partsSlice, 2)
	assert.Equal(t, "part-0", partsSlice[0].ID)
	assert.Equal(t, "echo hello", partsSlice[0].Text)
	assert.Equal(t, "builtin", partsSlice[0].Kind)
	assert.Equal(t, "print text", partsSlice[0].Semantic)
	assert.Equal(t, "Low", partsSlice[0].Risk)
	assert.Equal(t, "part-1", partsSlice[1].ID)
	assert.Equal(t, "rm -rf /tmp/data", partsSlice[1].Text)
	assert.Equal(t, "rm", partsSlice[1].Kind)
	assert.Equal(t, "recursive delete", partsSlice[1].Semantic)
	assert.Equal(t, "High", partsSlice[1].Risk)
}

func TestShellApprovalRequestEvent_JSONRoundTrip(t *testing.T) {
	parts := []ShellApprovalPartArg{
		{ID: "part-0", Text: "echo hello", Kind: "builtin", Semantic: "print text", Risk: "Low"},
		{ID: "part-1", Text: "rm -rf /tmp/data", Kind: "rm", Semantic: "recursive delete", Risk: "High"},
	}
	ev := ShellApprovalRequestEvent("req-1", "echo hello && rm -rf /tmp/data", parts, "echo hello\nrm -rf /tmp/data", "High")

	// Marshal to JSON (the event bus serializes map[string]interface{})
	raw, err := json.Marshal(ev)
	require.NoError(t, err)

	// Unmarshal back into a generic map
	var roundtrip map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &roundtrip))

	// Top-level fields survive the round-trip
	assert.Equal(t, "req-1", roundtrip["request_id"])
	assert.Equal(t, "echo hello && rm -rf /tmp/data", roundtrip["command"])
	assert.Equal(t, "echo hello\nrm -rf /tmp/data", roundtrip["unified_view"])
	assert.Equal(t, "High", roundtrip["risk_level"])
	assert.NotEmpty(t, roundtrip["timestamp"])

	// Parts survive as a JSON array of objects
	partsRaw, ok := roundtrip["parts"].([]interface{})
	require.True(t, ok, "parts should deserialize as []interface{}")
	assert.Len(t, partsRaw, 2)

	p0 := partsRaw[0].(map[string]interface{})
	assert.Equal(t, "part-0", p0["id"])
	assert.Equal(t, "echo hello", p0["text"])
	assert.Equal(t, "builtin", p0["kind"])
	assert.Equal(t, "print text", p0["semantic"])
	assert.Equal(t, "Low", p0["risk"])

	p1 := partsRaw[1].(map[string]interface{})
	assert.Equal(t, "part-1", p1["id"])
	assert.Equal(t, "rm -rf /tmp/data", p1["text"])
	assert.Equal(t, "rm", p1["kind"])
	assert.Equal(t, "recursive delete", p1["semantic"])
	assert.Equal(t, "High", p1["risk"])
}

func TestShellApprovalRequestEvent_EmptyParts(t *testing.T) {
	ev := ShellApprovalRequestEvent("req-2", "", []ShellApprovalPartArg{}, "", "")

	assert.Equal(t, "req-2", ev["request_id"])
	assert.Equal(t, "", ev["command"])
	assert.Empty(t, ev["parts"])
	assert.NotEmpty(t, ev["timestamp"])
}

func TestBuildShellApprovalUnifiedView(t *testing.T) {
	parts := []ShellApprovalPartArg{
		{ID: "part-0", Text: "echo hello", Kind: "builtin", Semantic: "print text", Risk: "Low"},
		{ID: "part-1", Text: "rm -rf foo", Kind: "rm", Semantic: "recursive delete", Risk: "High"},
	}

	view := BuildShellApprovalUnifiedView(parts)
	expected := "[builtin] echo hello  # print text\n[rm] rm -rf foo  # recursive delete"
	assert.Equal(t, expected, view)
}

func TestBuildShellApprovalUnifiedView_NoSemantic(t *testing.T) {
	parts := []ShellApprovalPartArg{
		{ID: "part-0", Text: "echo hi", Kind: "builtin", Semantic: "", Risk: "Low"},
	}
	view := BuildShellApprovalUnifiedView(parts)
	assert.Equal(t, "[builtin] echo hi", view)
}

func TestBuildShellApprovalUnifiedView_Empty(t *testing.T) {
	view := BuildShellApprovalUnifiedView(nil)
	assert.Empty(t, view)
}

func TestBuildShellApprovalUnifiedView_SinglePart(t *testing.T) {
	parts := []ShellApprovalPartArg{
		{ID: "only", Text: "ls", Kind: "unknown", Semantic: "list files", Risk: "Low"},
	}
	view := BuildShellApprovalUnifiedView(parts)
	assert.Equal(t, "[unknown] ls  # list files", view)
}

func TestShellApprovalResponsePayload_JSONRoundTrip(t *testing.T) {
	original := ShellApprovalResponsePayload{
		RequestID: "req-1",
		Decisions: map[string]bool{"part-0": true, "part-1": false},
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ShellApprovalResponsePayload
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, "req-1", decoded.RequestID)
	assert.Equal(t, true, decoded.Decisions["part-0"])
	assert.Equal(t, false, decoded.Decisions["part-1"])
}
