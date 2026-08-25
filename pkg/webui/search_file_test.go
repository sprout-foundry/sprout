//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestSearchFile_ContextAfter verifies the after-context second pass
// actually populates ContextAfter (the previous implementation appended
// to range-loop copies, silently discarding the lines) and collects the
// correct window for each match.
func TestSearchFile_ContextAfter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := "line one\nmatch A\nctx A1\nctx A2\nline five\nmatch B\nctx B1\nline eight\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ws := &ReactWebServer{}
	pattern := regexp.MustCompile(`match`)
	matches, count, err := ws.searchFile(path, pattern, 2)
	if err != nil {
		t.Fatalf("searchFile: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}

	// Match A at line 2: after-context should be lines 3 and 4.
	if matches[0].LineNumber != 2 {
		t.Fatalf("match 0 line = %d, want 2", matches[0].LineNumber)
	}
	if len(matches[0].ContextAfter) != 2 {
		t.Fatalf("match 0 after = %v, want 2 lines", matches[0].ContextAfter)
	}
	if matches[0].ContextAfter[0] != "ctx A1" || matches[0].ContextAfter[1] != "ctx A2" {
		t.Errorf("match 0 after = %v, want [ctx A1 ctx A2]", matches[0].ContextAfter)
	}

	// Match B at line 6: context 2 covers lines 7 and 8.
	if matches[1].LineNumber != 6 {
		t.Fatalf("match 1 line = %d, want 6", matches[1].LineNumber)
	}
	if len(matches[1].ContextAfter) != 2 || matches[1].ContextAfter[0] != "ctx B1" || matches[1].ContextAfter[1] != "line eight" {
		t.Errorf("match 1 after = %v, want [ctx B1 line eight]", matches[1].ContextAfter)
	}
}

// TestSearchFile_ContextBeforeAndAfter verifies both context sides are
// collected and before-context does not include the match line itself.
func TestSearchFile_ContextBeforeAndAfter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := "pre 1\npre 2\npre 3\nMATCH HERE\npost 1\npost 2\npost 3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ws := &ReactWebServer{}
	pattern := regexp.MustCompile(`MATCH`)
	matches, _, err := ws.searchFile(path, pattern, 3)
	if err != nil {
		t.Fatalf("searchFile: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	m := matches[0]
	if m.LineNumber != 4 {
		t.Fatalf("line = %d, want 4", m.LineNumber)
	}
	if len(m.ContextBefore) != 3 || m.ContextBefore[0] != "pre 1" || m.ContextBefore[2] != "pre 3" {
		t.Errorf("before = %v, want [pre 1 pre 2 pre 3]", m.ContextBefore)
	}
	if len(m.ContextAfter) != 3 || m.ContextAfter[0] != "post 1" || m.ContextAfter[2] != "post 3" {
		t.Errorf("after = %v, want [post 1 post 2 post 3]", m.ContextAfter)
	}
}

// TestSearchFile_AdjacentMatches exercises the sliding window when matches
// are close together: each match gets its own window without leaking
// another match's lines.
func TestSearchFile_AdjacentMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := "hit one\nhit two\nbetween\nhit three\nafter\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ws := &ReactWebServer{}
	pattern := regexp.MustCompile(`hit`)
	matches, _, err := ws.searchFile(path, pattern, 1)
	if err != nil {
		t.Fatalf("searchFile: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(matches))
	}

	// Match 1 (line 1) window: line 2.
	if matches[0].LineNumber != 1 || len(matches[0].ContextAfter) != 1 || matches[0].ContextAfter[0] != "hit two" {
		t.Errorf("match 0 = %+v", matches[0])
	}
	// Match 2 (line 2) window: line 3.
	if matches[1].LineNumber != 2 || len(matches[1].ContextAfter) != 1 || matches[1].ContextAfter[0] != "between" {
		t.Errorf("match 1 = %+v", matches[1])
	}
	// Match 3 (line 4) window: line 5.
	if matches[2].LineNumber != 4 || len(matches[2].ContextAfter) != 1 || matches[2].ContextAfter[0] != "after" {
		t.Errorf("match 2 = %+v", matches[2])
	}
}
