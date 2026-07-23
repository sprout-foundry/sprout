package automate

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSummarize_AllowedPaths exercises the happy path for the
// allowed_paths summary field. A workflow with well-formed entries
// must populate Summary.AllowedPaths with the right mode + reason
// (SP-128-1c). Entries are sorted by path for stable display.
func TestSummarize_AllowedPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	// Note: deliberately unsorted input paths so the test exercises
	// the sort-by-path normalization.
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/var/log/sprout", "mode": "read_only", "reason": "Tail logs"},
			{"path": "/srv/datasets", "mode": "read_write", "reason": "Read training data"}
		]
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.AllowedPaths) != 2 {
		t.Fatalf("expected 2 allowed_paths entries, got %d", len(s.AllowedPaths))
	}
	// Sorted ascending by path: /srv/datasets < /var/log/sprout.
	if s.AllowedPaths[0].Path != "/srv/datasets" {
		t.Errorf("entries should be sorted by path; got %q first", s.AllowedPaths[0].Path)
	}
	if s.AllowedPaths[0].Mode != "read_write" || s.AllowedPaths[0].Reason != "Read training data" {
		t.Errorf("entry[0] = %+v; want mode=read_write reason=Read training data", s.AllowedPaths[0])
	}
	if s.AllowedPaths[1].Path != "/var/log/sprout" || s.AllowedPaths[1].Mode != "read_only" || s.AllowedPaths[1].Reason != "Tail logs" {
		t.Errorf("entry[1] = %+v; want path=/var/log/sprout mode=read_only reason=Tail logs", s.AllowedPaths[1])
	}
}

// TestSummarize_AllowedPaths_BadPath verifies that a malformed
// allowed_paths entry surfaces as a parse error (SP-128-1c — "a
// malformed entry surfaces as a parse error"). The summary parser
// must NOT silently drop the whole allowed_paths field when one
// entry is bad; instead the error must identify the offending
// index so the workflow author can fix it.
func TestSummarize_AllowedPaths_BadPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "read_write"},
			{"path": "relative/path", "mode": "read_only"}
		]
	}`)

	_, err := Summarize(path)
	if err == nil {
		t.Fatal("expected Summarize to fail on relative path; got nil")
	}
	if !strings.Contains(err.Error(), "allowed_paths[1]") {
		t.Fatalf("error should identify offending index 1, got: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should mention 'absolute', got: %v", err)
	}
}

// TestSummarize_AllowedPaths_BadMode verifies the mode-enum rule
// applies at Summarize time (mirrors the loader's behavior — both
// must reject "rw" and friends).
func TestSummarize_AllowedPaths_BadMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "rw"}
		]
	}`)

	_, err := Summarize(path)
	if err == nil {
		t.Fatal("expected Summarize to fail on bad mode; got nil")
	}
	if !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("error should mention 'mode must be', got: %v", err)
	}
}

// TestSummarize_AllowedPaths_SystemPrefixWarning exercises the
// advisory path: an allowed_paths entry under /etc (or any other
// system prefix) is allowed to summarize, but the Summary.Warnings
// slice carries a human-readable warning so the CLI / WebUI can
// surface the heads-up next to the External Paths section.
func TestSummarize_AllowedPaths_SystemPrefixWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/etc/sprout-stuff", "mode": "read_only"}
		]
	}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.AllowedPaths) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(s.AllowedPaths))
	}
	if len(s.Warnings) == 0 {
		t.Fatal("expected Warnings slice to be populated for system prefix")
	}
	joined := strings.Join(s.Warnings, "\n")
	if !strings.Contains(joined, "/etc/sprout-stuff") {
		t.Errorf("warning should mention the offending path, got: %s", joined)
	}
	if !strings.Contains(joined, "system prefix") {
		t.Errorf("warning should mention 'system prefix', got: %s", joined)
	}
}

// TestSummarize_AllowedPaths_Empty exercises the empty case: a
// workflow with no allowed_paths section must not error and must
// produce an empty summary slice.
func TestSummarize_AllowedPaths_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	mustWriteFile(t, path, `{"initial": {"prompt": "x"}}`)

	s, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(s.AllowedPaths) != 0 {
		t.Errorf("expected empty AllowedPaths, got %d entries", len(s.AllowedPaths))
	}
	if len(s.Warnings) != 0 {
		t.Errorf("expected empty Warnings, got %v", s.Warnings)
	}
}
