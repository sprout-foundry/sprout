package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot locates the repository root (the directory containing go.mod) by
// walking up from the test binary's working directory, so the test works
// both under `go test ./cmd/...` from the root and `go test .` from inside
// the package directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root (no go.mod found)")
		}
		dir = fallbackDir(dir, parent)
	}
}

// fallbackDir keeps the walk simple; it exists as a named hook so the loop
// body stays a one-liner.
func fallbackDir(_, parent string) string { return parent }

// buildFromRepo derives the snapshot against the real checked-in registry.
func buildFromRepo(t *testing.T) []studioProvider {
	t.Helper()
	root := repoRoot(t)
	snapshot, err := buildStudioProviders(
		filepath.Join(root, "pkg", "agent_providers", "configs"),
		filepath.Join(root, "pkg", "providercatalog", "providers.json"),
	)
	if err != nil {
		t.Fatalf("buildStudioProviders: %v", err)
	}
	return snapshot
}

func TestGenerateStudioProvidersSnapshotShape(t *testing.T) {
	snapshot := buildFromRepo(t)

	if len(snapshot) != 13 {
		t.Fatalf("expected 13 providers, got %d", len(snapshot))
	}

	wantFields := []string{
		"id", "name", "envVar", "requiresKey", "recommended",
		"description", "docsUrl", "signupUrl", "apiKeyLabel", "apiKeyHelp",
		"setupHint", "recommendedModel", "recommendedModelWhy", "models",
	}

	byID := make(map[string]studioProvider, len(snapshot))
	for _, p := range snapshot {
		// Every object must carry all 14 fields, even when empty — the
		// bridge reads p.models.length and friends unconditionally.
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal %s: %v", p.ID, err)
		}
		var asMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &asMap); err != nil {
			t.Fatalf("unmarshal %s: %v", p.ID, err)
		}
		for _, field := range wantFields {
			if _, ok := asMap[field]; !ok {
				t.Errorf("provider %s is missing field %q (json: %s)", p.ID, field, raw)
			}
		}

		if p.Models == nil {
			t.Errorf("provider %s: models must be non-nil so it marshals as [] not null", p.ID)
		}
		if _, dup := byID[p.ID]; dup {
			t.Errorf("duplicate provider id %q", p.ID)
		}
		byID[p.ID] = p
	}

	// Spot-check providers whose model lists come from different sources:
	// zai/deepinfra from models.model_info, sprout-local from the catalog
	// fallback (its config declares no models).
	for _, id := range []string{"zai", "deepinfra"} {
		if len(byID[id].Models) == 0 {
			t.Errorf("provider %s: expected non-empty model list", id)
		}
	}
	if len(byID["sprout-local"].Models) == 0 {
		t.Errorf("provider sprout-local: expected catalog-fallback model list to be non-empty")
	}

	var orModels []string
	for _, p := range snapshot {
		if p.ID == "openrouter" {
			orModels = p.Models
		}
	}
	if orModels == nil {
		t.Fatal("provider openrouter missing from snapshot")
	}
	foundGLM := false
	for _, m := range orModels {
		if m == "z-ai/glm-5.3" {
			foundGLM = true
		}
		// openrouter/free IS a real, user-selectable OpenRouter model (the
		// current snapshot ships it); only the negative-cost routing aliases
		// are dropped, which the shape assertions above already cover via
		// the negative-cost filter.
		for _, alias := range []string{"openrouter/auto", "openrouter/auto-beta", "openrouter/bodybuilder", "openrouter/fusion", "openrouter/pareto-code"} {
			if m == alias {
				t.Errorf("openrouter models contain routing alias %q", m)
			}
		}
	}
	if !foundGLM {
		t.Errorf("openrouter models do not contain z-ai/glm-5.3")
	}
}

func TestGenerateStudioProvidersPrettyJSONOutput(t *testing.T) {
	snapshot := buildFromRepo(t)

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "studio-providers.json")
	if err := writeJSONFile(outPath, snapshot); err != nil {
		t.Fatalf("writeJSONFile: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Pretty-printed, 2-space indent, and round-trips to the same snapshot.
	if !strings.HasPrefix(string(data), "[\n  {") {
		t.Errorf("expected 2-space pretty-printed JSON array, got prefix %q", firstN(string(data), 20))
	}

	var decoded []studioProvider
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(decoded) != 13 {
		t.Errorf("expected 13 providers in JSON output, got %d", len(decoded))
	}
}

// fakeBridge is a miniature stand-in for studio-bridge.js with the same
// STUDIO_PROVIDERS line shape (4-space indent, trailing newline).
const fakeBridge = `(function () {
    'use strict';

    var BEFORE = 1;

    var STUDIO_PROVIDERS = [{"id":"old","name":"Old","envVar":"","requiresKey":false,"recommended":false,"description":"","docsUrl":"","signupUrl":"","apiKeyLabel":"","apiKeyHelp":"","setupHint":"","recommendedModel":"","recommendedModelWhy":"","models":[]}];

    function studioFindProvider(id) {
        for (var i = 0; i < STUDIO_PROVIDERS.length; i++) {
            if (STUDIO_PROVIDERS[i].id === id) return STUDIO_PROVIDERS[i];
        }
        return null;
    }

    var AFTER = studioFindProvider('old');
    return AFTER;
})();
`

func TestRewriteBridgeSnapshotReplacesOnlyTargetLine(t *testing.T) {
	snapshot := []studioProvider{{
		ID:                  "zai",
		Name:                "Z.AI",
		EnvVar:              "ZAI_API_KEY",
		RequiresKey:         true,
		Recommended:         true,
		Description:         "desc with — em dash & <angle> \"quotes\"",
		DocsURL:             "https://docs.example",
		SignupURL:           "https://signup.example",
		APIKeyLabel:         "Z.AI API Key",
		APIKeyHelp:          "help",
		SetupHint:           "hint",
		RecommendedModel:    "glm-5.3",
		RecommendedModelWhy: "why",
		Models:              []string{"glm-5.2", "glm-5.3"},
	}}

	dir := t.TempDir()
	bridgePath := filepath.Join(dir, "studio-bridge.js")
	if err := os.WriteFile(bridgePath, []byte(fakeBridge), 0o644); err != nil {
		t.Fatalf("write fake bridge: %v", err)
	}

	if err := rewriteBridgeSnapshot(bridgePath, snapshot); err != nil {
		t.Fatalf("rewriteBridgeSnapshot: %v", err)
	}

	raw, err := os.ReadFile(bridgePath)
	if err != nil {
		t.Fatalf("read rewritten bridge: %v", err)
	}
	got := string(raw)

	// Everything outside the target line survives byte-for-byte.
	wantPrefix := "(function () {\n    'use strict';\n\n    var BEFORE = 1;\n\n    "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("bytes before the snapshot line changed.\nwant prefix: %q\ngot prefix:  %q", wantPrefix, firstN(got, len(wantPrefix)))
	}
	wantSuffix := "\n\n    function studioFindProvider(id) {\n"
	if !strings.Contains(got, wantSuffix) {
		t.Errorf("bytes after the snapshot line changed; missing %q", wantSuffix)
	}
	if !strings.HasSuffix(got, "})();\n") {
		t.Errorf("file tail changed; got tail %q", lastN(got, 10))
	}

	// The rewritten line parses as JSON and carries the new snapshot.
	start := strings.Index(got, "var STUDIO_PROVIDERS = ")
	end := strings.Index(got, "];")
	if start == -1 || end == -1 || end < start {
		t.Fatalf("could not locate rewritten snapshot line")
	}
	jsonPart := got[start+len("var STUDIO_PROVIDERS = ") : end+1]
	var decoded []studioProvider
	if err := json.Unmarshal([]byte(jsonPart), &decoded); err != nil {
		t.Fatalf("rewritten line is not valid JSON: %v\nline: %s", err, jsonPart)
	}
	if len(decoded) != 1 || decoded[0].ID != "zai" || decoded[0].RecommendedModel != "glm-5.3" {
		t.Errorf("rewritten snapshot has unexpected content: %+v", decoded)
	}
	if decoded[0].Description != snapshot[0].Description {
		t.Errorf("unicode/punctuation copy did not survive: got %q", decoded[0].Description)
	}
	if !strings.HasPrefix(strings.TrimSpace(lineWith(got, "var STUDIO_PROVIDERS")), "var STUDIO_PROVIDERS") {
		t.Errorf("rewritten line lost its indentation")
	}

	// The snapshot line must be a single physical line.
	markerLine := lineWith(got, "var STUDIO_PROVIDERS")
	if strings.ContainsAny(markerLine, "\r") {
		t.Errorf("rewritten line contains a CR")
	}

	// Idempotent: rewriting again with the same payload changes nothing.
	before, err := os.ReadFile(bridgePath)
	if err != nil {
		t.Fatalf("re-read bridge: %v", err)
	}
	if err := rewriteBridgeSnapshot(bridgePath, snapshot); err != nil {
		t.Fatalf("second rewriteBridgeSnapshot: %v", err)
	}
	after, err := os.ReadFile(bridgePath)
	if err != nil {
		t.Fatalf("re-read bridge: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("rewrite is not idempotent")
	}
}

func TestRewriteBridgeSnapshotErrors(t *testing.T) {
	snapshot := []studioProvider{{ID: "x", Models: []string{}}}
	dir := t.TempDir()

	t.Run("missing marker", func(t *testing.T) {
		path := filepath.Join(dir, "no-marker.js")
		if err := os.WriteFile(path, []byte("var OTHER = [];\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := rewriteBridgeSnapshot(path, snapshot); err == nil {
			t.Fatal("expected error when the marker line is absent")
		}
	})

	t.Run("duplicate marker", func(t *testing.T) {
		path := filepath.Join(dir, "dup-marker.js")
		content := "var A = 1;\n    var STUDIO_PROVIDERS = [1];\nvar B = 2;\n    var STUDIO_PROVIDERS = [2];\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := rewriteBridgeSnapshot(path, snapshot); err == nil {
			t.Fatal("expected error when the marker appears twice")
		}
	})

	t.Run("malformed snapshot line", func(t *testing.T) {
		path := filepath.Join(dir, "malformed.js")
		content := "var A = 1;\n    var STUDIO_PROVIDERS = not-an-array;\nvar B = 2;\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := rewriteBridgeSnapshot(path, snapshot); err == nil {
			t.Fatal("expected error when the marker line is not `[...];`")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if err := rewriteBridgeSnapshot(filepath.Join(dir, "absent.js"), snapshot); err == nil {
			t.Fatal("expected error when the bridge file does not exist")
		}
	})
}

// TestNormalizeModelIDs pins the ordering/dedup/alias rules. Dedup is
// case-sensitive on purpose: model IDs are case-sensitive identifiers, and
// the mixed-case GLM entries in the old hand-maintained snapshot (e.g.
// "GLM-4.5" next to "glm-4.6v") were precisely the drift this generator
// eliminates.
func TestNormalizeModelIDs(t *testing.T) {
	got := normalizeModelIDs([]string{
		"glm-5.3", "glm-5", "glm-5", " openrouter/auto ", "", "GLM-5", "B-Model", "a-model",
	})
	want := []string{"a-model", "B-Model", "glm-5", "GLM-5", "glm-5.3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func lineWith(s, marker string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func lastN(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
