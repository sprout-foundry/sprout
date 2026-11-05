package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestApprovedMCPToolsRoundTrip pins JSON serialization of the SP-170
// allowlist: the field's omitempty must not collapse a populated list, and
// the key must stay stable (users hand-edit the file).
func TestApprovedMCPToolsRoundTrip(t *testing.T) {
	cfg := Config{
		ApprovedMCPTools: []string{"github", "gitlab__create_issue"},
	}
	data, marshalErr := json.Marshal(cfg)
	if marshalErr != nil {
		t.Fatalf("Marshal: %v", marshalErr)
	}
	if !contains(string(data), `"approved_mcp_tools":["github","gitlab__create_issue"]`) {
		t.Errorf("unexpected JSON form: %s", data)
	}

	var roundTripped Config
	if unmarshalErr := json.Unmarshal(data, &roundTripped); unmarshalErr != nil {
		t.Fatalf("Unmarshal: %v", unmarshalErr)
	}
	if len(roundTripped.ApprovedMCPTools) != 2 {
		t.Errorf("after round-trip: got %d entries, want 2", len(roundTripped.ApprovedMCPTools))
	}
}

func TestApprovedMCPToolsEmptyOmitted(t *testing.T) {
	data, marshalErr := json.Marshal(Config{})
	if marshalErr != nil {
		t.Fatalf("Marshal: %v", marshalErr)
	}
	if contains(string(data), "approved_mcp_tools") {
		t.Errorf("empty list should be omitted via omitempty, got: %s", data)
	}
}

// TestApprovedMCPToolsMerge_Union confirms workspace-layer entries stack on
// the global layer with de-dup. Without this branch a layered load would
// silently forget an allowlist entry and start denying an approved tool.
func TestApprovedMCPToolsMerge_Union(t *testing.T) {
	tmpRoot := t.TempDir()
	globalDir := filepath.Join(tmpRoot, "global")
	workspaceDir := filepath.Join(tmpRoot, "workspace", ".sprout")
	if mkErr := os.MkdirAll(globalDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	if mkErr := os.MkdirAll(workspaceDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}

	global := Config{ApprovedMCPTools: []string{"github", "gitlab__create_issue"}}
	workspace := Config{ApprovedMCPTools: []string{"gitlab__create_issue", "acme"}}

	merged := MergeConfig(&global, &workspace)

	want := map[string]int{"github": 1, "gitlab__create_issue": 1, "acme": 1}
	got := map[string]int{}
	for _, e := range merged.ApprovedMCPTools {
		got[e]++
	}
	for entry, count := range want {
		if got[entry] != count {
			t.Errorf("merged entry %q: got %d occurrences, want %d (merged=%v)",
				entry, got[entry], count, merged.ApprovedMCPTools)
		}
	}
	if len(merged.ApprovedMCPTools) != 3 {
		t.Errorf("merged list = %v, want 3 unique entries", merged.ApprovedMCPTools)
	}
}
