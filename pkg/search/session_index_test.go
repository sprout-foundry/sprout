package search

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeSession(t *testing.T, dir, sessionID string, s sessionJSON) string {
	t.Helper()
	s.SessionID = sessionID
	dir = filepath.Join(dir, "scoped", "abcdef12")
	os.MkdirAll(dir, 0755) //nolint:errcheck // best-effort in tests
	path := filepath.Join(dir, "session_"+sessionID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal session JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}

func writeSessionWithMtime(t *testing.T, dir, sessionID string, s sessionJSON, mtime time.Time) string {
	t.Helper()
	path := writeSession(t, dir, sessionID, s)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return path
}

func makeBaseSession(name, workingDir string) sessionJSON {
	return sessionJSON{
		Name:             name,
		WorkingDirectory: workingDir,
		TotalCost:        0.05,
		Messages: []messageRef{
			{Role: "user", Content: "Hello there"},
			{Role: "assistant", Content: "Hi! How can I help?"},
		},
	}
}

// ---------------------------------------------------------------------------
// 1. BuildFromScratch
// ---------------------------------------------------------------------------

func TestSessionIndex_BuildFromScratch(t *testing.T) {
	tmp := t.TempDir()
	ses1 := writeSession(t, tmp, "ses-1", makeBaseSession("First Session", "/home/user/proj"))
	ses2 := writeSession(t, tmp, "ses-2", makeBaseSession("Second Session", "/home/user/other"))
	ses3 := writeSession(t, tmp, "ses-3", sessionJSON{
		Name:             "Third Session",
		WorkingDirectory: "/tmp",
		TotalCost:        0.10,
		Messages: []messageRef{
			{Role: "user", Content: "What is Go?"},
			{Role: "assistant", Content: "Go is a compiled language."},
			{Role: "tool", Content: "tool output"},
			{Role: "assistant", Content: "It was developed at Google."},
		},
	})

	idx, err := BuildIndex(tmp, nil)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	// Exactly 3 entries.
	if len(idx.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(idx.Sessions))
	}

	// Version should be 1.
	if idx.Version != 1 {
		t.Errorf("expected version 1, got %d", idx.Version)
	}

	// BuiltAt should be recent.
	if time.Since(idx.BuiltAt) > 5*time.Second {
		t.Errorf("BuiltAt is too old: %v", idx.BuiltAt)
	}

	for id, want := range map[string]struct {
		name       string
		workingDir string
		msgCount   int
	}{
		"ses-1": {"First Session", "/home/user/proj", 2},
		"ses-2": {"Second Session", "/home/user/other", 2},
		"ses-3": {"Third Session", "/tmp", 3}, // 2 user+assistant + 1 assistant (tool is excluded)
	} {
		e, ok := idx.Sessions[id]
		if !ok {
			t.Errorf("missing session %q", id)
			continue
		}
		if e.Name != want.name {
			t.Errorf("%s: name = %q, want %q", id, e.Name, want.name)
		}
		if e.WorkingDir != want.workingDir {
			t.Errorf("%s: workingDir = %q, want %q", id, e.WorkingDir, want.workingDir)
		}
		if e.MessageCount != want.msgCount {
			t.Errorf("%s: messageCount = %d, want %d", id, e.MessageCount, want.msgCount)
		}
		if e.Text == "" {
			t.Errorf("%s: Text is empty", id)
		}
		// Verify Text is lowercased.
		if e.Text != strings.ToLower(e.Text) {
			t.Errorf("%s: Text is not lowercased: %q", id, e.Text)
		}
	}

	// Verify the session files still exist (sanity check).
	for _, f := range []string{ses1, ses2, ses3} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("session file %q missing after build: %v", f, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. LoadSaveRoundTrip
// ---------------------------------------------------------------------------

func TestSessionIndex_LoadSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "search-index.json")

	// Create an index with known entries.
	now := time.Now().Truncate(time.Second)
	idx := &SessionIndex{
		Version: 1,
		BuiltAt: now,
		Sessions: map[string]SessionIndexEntry{
			"alpha": {
				SessionID:    "alpha",
				Name:         "Alpha Session",
				WorkingDir:   "/home/user",
				LastUpdated:  now,
				TotalCost:    0.05,
				MessageCount: 2,
				Text:         "hello world",
				Tokens:       map[string][]int{"alpha:0": {0, 5}, "alpha:1": {5, 11}},
			},
			"beta": {
				SessionID:    "beta",
				Name:         "Beta Session",
				WorkingDir:   "/tmp",
				LastUpdated:  now.Add(time.Hour),
				TotalCost:    0.1,
				MessageCount: 1,
				Text:         "gopher lang",
				Tokens:       map[string][]int{"beta:0": {0, 11}},
			},
		},
	}

	if err := SaveIndex(path, idx); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	loaded, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(loaded.Sessions) != len(idx.Sessions) {
		t.Fatalf("session count: got %d, want %d", len(loaded.Sessions), len(idx.Sessions))
	}

	for id, want := range idx.Sessions {
		got, ok := loaded.Sessions[id]
		if !ok {
			t.Errorf("missing session %q after round trip", id)
			continue
		}

		if got.SessionID != want.SessionID {
			t.Errorf("%s: SessionID = %q, want %q", id, got.SessionID, want.SessionID)
		}
		if got.Name != want.Name {
			t.Errorf("%s: Name = %q, want %q", id, got.Name, want.Name)
		}
		if got.WorkingDir != want.WorkingDir {
			t.Errorf("%s: WorkingDir = %q, want %q", id, got.WorkingDir, want.WorkingDir)
		}
		if !got.LastUpdated.Equal(want.LastUpdated) {
			t.Errorf("%s: LastUpdated = %v, want %v", id, got.LastUpdated, want.LastUpdated)
		}
		if got.TotalCost != want.TotalCost {
			t.Errorf("%s: TotalCost = %v, want %v", id, got.TotalCost, want.TotalCost)
		}
		if got.MessageCount != want.MessageCount {
			t.Errorf("%s: MessageCount = %d, want %d", id, got.MessageCount, want.MessageCount)
		}
		if got.Text != want.Text {
			t.Errorf("%s: Text = %q, want %q", id, got.Text, want.Text)
		}
		if len(got.Tokens) != len(want.Tokens) {
			t.Errorf("%s: Tokens length = %d, want %d", id, len(got.Tokens), len(want.Tokens))
		}
		for k, wv := range want.Tokens {
			gv, ok := got.Tokens[k]
			if !ok {
				t.Errorf("%s: missing token key %q", id, k)
				continue
			}
			if len(gv) != len(wv) || gv[0] != wv[0] || gv[1] != wv[1] {
				t.Errorf("%s: token %v = %v, want %v", id, k, gv, wv)
			}
		}
	}

	// Sessions map should not be nil.
	if loaded.Sessions == nil {
		t.Error("Sessions map is nil after load")
	}

	// BuiltAt is updated by SaveIndex, so compare loosely.
	if loaded.BuiltAt.Before(now) {
		t.Errorf("BuiltAt %v is before original %v", loaded.BuiltAt, now)
	}

	// Version preserved.
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
}

// ---------------------------------------------------------------------------
// 3. IncrementalUpdate
// ---------------------------------------------------------------------------

func TestSessionIndex_IncrementalUpdate(t *testing.T) {
	tmp := t.TempDir()
	baseTime := time.Now().Add(-10 * time.Second)
	modifiedTime := time.Now()

	// Create two session files with distinct mtimes.
	path1 := writeSessionWithMtime(t, tmp, "ses-1", makeBaseSession("Session One", "/a"), baseTime)
	writeSessionWithMtime(t, tmp, "ses-2", makeBaseSession("Session Two", "/b"), baseTime)

	// First build.
	idx, err := BuildIndex(tmp, nil)
	if err != nil {
		t.Fatalf("first BuildIndex: %v", err)
	}
	text1Before := idx.Sessions["ses-1"].Text
	text2Before := idx.Sessions["ses-2"].Text
	updated2Before := idx.Sessions["ses-2"].LastUpdated

	// Wait a second, then modify ses-1's content.
	time.Sleep(time.Second)
	newContent := sessionJSON{
		Name:             "Session One Modified",
		WorkingDirectory: "/a",
		Messages: []messageRef{
			{Role: "user", Content: "New question"},
			{Role: "assistant", Content: "New answer here"},
		},
	}
	newContent.SessionID = "ses-1"
	data, err := json.MarshalIndent(newContent, "", "  ")
	if err != nil {
		t.Fatalf("marshal modified: %v", err)
	}
	if err := os.WriteFile(path1, data, 0644); err != nil {
		t.Fatalf("write modified: %v", err)
	}
	if err := os.Chtimes(path1, modifiedTime, modifiedTime); err != nil {
		t.Fatalf("chtimes modified: %v", err)
	}

	// Second build — incremental.
	idx2, err := BuildIndex(tmp, idx)
	if err != nil {
		t.Fatalf("second BuildIndex: %v", err)
	}

	// ses-1 should have been rebuilt (text changed).
	e1 := idx2.Sessions["ses-1"]
	if e1.Text == text1Before {
		t.Fatalf("ses-1 Text did not change after file modification")
	}
	if e1.LastUpdated.Equal(baseTime) {
		t.Error("ses-1 LastUpdated still equals original mtime after modification")
	}

	// ses-2 should NOT have been rebuilt.
	e2 := idx2.Sessions["ses-2"]
	if e2.Text != text2Before {
		t.Errorf("ses-2 Text changed unexpectedly: got %q, want %q", e2.Text, text2Before)
	}
	if !e2.LastUpdated.Equal(updated2Before) {
		t.Errorf("ses-2 LastUpdated changed unexpectedly: got %v, want %v", e2.LastUpdated, updated2Before)
	}
}

// ---------------------------------------------------------------------------
// 4. DropMissing
// ---------------------------------------------------------------------------

func TestSessionIndex_DropMissing(t *testing.T) {
	tmp := t.TempDir()
	writeSession(t, tmp, "ses-1", makeBaseSession("One", "/a"))
	ses2 := writeSession(t, tmp, "ses-2", makeBaseSession("Two", "/b"))
	writeSession(t, tmp, "ses-3", makeBaseSession("Three", "/c"))

	idx, err := BuildIndex(tmp, nil)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if len(idx.Sessions) != 3 {
		t.Fatalf("initial build: expected 3, got %d", len(idx.Sessions))
	}

	// Delete one session file.
	if err := os.Remove(ses2); err != nil {
		t.Fatalf("remove session file: %v", err)
	}

	// Rebuild.
	idx2, err := BuildIndex(tmp, idx)
	if err != nil {
		t.Fatalf("rebuild after delete: %v", err)
	}

	// ses-2 should be gone.
	if _, ok := idx2.Sessions["ses-2"]; ok {
		t.Error("deleted session ses-2 still in index")
	}

	// ses-1 and ses-3 should remain.
	if len(idx2.Sessions) != 2 {
		t.Errorf("expected 2 sessions after drop, got %d", len(idx2.Sessions))
	}
	if _, ok := idx2.Sessions["ses-1"]; !ok {
		t.Error("ses-1 missing from index")
	}
	if _, ok := idx2.Sessions["ses-3"]; !ok {
		t.Error("ses-3 missing from index")
	}

	// Verify unchanged sessions still have correct data.
	e1 := idx2.Sessions["ses-1"]
	if e1.Name != "One" {
		t.Errorf("ses-1 Name = %q, want %q", e1.Name, "One")
	}
	e3 := idx2.Sessions["ses-3"]
	if e3.Name != "Three" {
		t.Errorf("ses-3 Name = %q, want %q", e3.Name, "Three")
	}
}

// ---------------------------------------------------------------------------
// 5. LoadMissingFile
// ---------------------------------------------------------------------------

func TestSessionIndex_LoadMissingFile(t *testing.T) {
	idx, err := LoadIndex("/nonexistent/path/search-index.json")
	if err != nil {
		t.Fatalf("LoadIndex on missing file returned error: %v", err)
	}
	if idx == nil {
		t.Fatal("LoadIndex returned nil index")
	}
	if idx.Sessions == nil {
		t.Fatal("Sessions map is nil after loading missing file")
	}
	if len(idx.Sessions) != 0 {
		t.Errorf("expected empty Sessions map, got %d entries", len(idx.Sessions))
	}
}

// ---------------------------------------------------------------------------
// 6. LoadMalformedFile
// ---------------------------------------------------------------------------

func TestSessionIndex_LoadMalformedFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "search-index.json")

	if err := os.WriteFile(path, []byte("not json at all {{{"), 0644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	_, err := LoadIndex(path)
	if err == nil {
		t.Fatal("LoadIndex on malformed file returned no error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 7. AtomicSave — concurrent reads/writes never see corrupt data
// ---------------------------------------------------------------------------

func TestSessionIndex_AtomicSave(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "search-index.json")

	var wg sync.WaitGroup
	errs := make(chan string, 100)

	// Writer goroutine: saves 20 times rapidly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			idx := &SessionIndex{
				Version: 1,
				Sessions: map[string]SessionIndexEntry{
					"iter-" + string(rune('a'+i%26)): {
						SessionID:    "s",
						Name:         "Iteration",
						MessageCount: i,
						Text:         strings.Repeat("x", 100),
					},
				},
			}
			if err := SaveIndex(path, idx); err != nil {
				errs <- fmt.Sprintf("save %d: %v", i, err)
			}
		}
	}()

	// Reader goroutines: 5 goroutines each doing 50 reads.
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				idx, err := LoadIndex(path)
				if err != nil {
					// Missing file is OK during racing; but parse errors are not.
					if !os.IsNotExist(err) {
						errs <- fmt.Sprintf("load %d: %v", i, err)
					}
					continue
				}
				if idx == nil || idx.Sessions == nil {
					errs <- fmt.Sprintf("load %d: nil index or sessions", i)
					continue
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Errorf("atomic save/load error: %s", e)
	}
}

// ---------------------------------------------------------------------------
// 8. DefaultIndexPath
// ---------------------------------------------------------------------------

func TestSessionIndex_DefaultIndexPath(t *testing.T) {
	path := DefaultIndexPath()
	if path == "" {
		t.Fatal("DefaultIndexPath returned empty string")
	}
	if !strings.Contains(path, "search-index.json") {
		t.Errorf("path should contain 'search-index.json': %q", path)
	}
	if !strings.Contains(path, ".sprout/sessions/") {
		t.Errorf("path should contain '.sprout/sessions/': %q", path)
	}
}

// ---------------------------------------------------------------------------
// 9. TokenOffsets
// ---------------------------------------------------------------------------

func TestSessionIndex_TokenOffsets(t *testing.T) {
	tmp := t.TempDir()
	s := sessionJSON{
		Name: "Token Test",
		Messages: []messageRef{
			{Role: "user", Content: "Hello world"},
			{Role: "assistant", Content: "Hi there"},
			{Role: "user", Content: "How are you"},
		},
	}
	writeSession(t, tmp, "tok-ses", s)

	idx, err := BuildIndex(tmp, nil)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	e, ok := idx.Sessions["tok-ses"]
	if !ok {
		t.Fatal("session not found in index")
	}

	// MessageCount should be 3 (all user/assistant).
	if e.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", e.MessageCount)
	}

	// Tokens map should have 3 entries.
	if len(e.Tokens) != 3 {
		t.Fatalf("Tokens has %d entries, want 3", len(e.Tokens))
	}

	// Verify each token range correctly slices out the lowercased content.
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("tok-ses:%d", i)
		token, ok := e.Tokens[key]
		if !ok {
			t.Errorf("missing token key %q", key)
			continue
		}
		if len(token) != 2 {
			t.Errorf("token %v: expected 2 elements, got %d", key, len(token))
			continue
		}
		start, end := token[0], token[1]
		if start < 0 || end > len(e.Text) || start > end {
			t.Errorf("token %v: invalid range [%d, %d] in text of length %d", key, start, end, len(e.Text))
			continue
		}
		extracted := e.Text[start:end]
		expected := strings.ToLower(s.Messages[i].Content)
		if extracted != expected {
			t.Errorf("token %v: extracted %q, want %q", key, extracted, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// 10. WalkSessions
// ---------------------------------------------------------------------------

func TestSessionIndex_WalkSessions(t *testing.T) {
	tmp := t.TempDir()

	// Create session files in nested scoped dirs.
	writeSession(t, tmp, "aaa", makeBaseSession("A", "/a"))
	writeSession(t, tmp, "bbb", makeBaseSession("B", "/b"))

	// Add a non-session file that should be ignored.
	os.WriteFile(filepath.Join(tmp, "scoped", "abcdef12", "notes.txt"), []byte("hi"), 0644) //nolint:errcheck

	files, err := WalkSessions(tmp)
	if err != nil {
		t.Fatalf("WalkSessions: %v", err)
	}

	// Should have exactly 2 session files.
	if len(files) != 2 {
		t.Fatalf("WalkSessions returned %d files, want 2", len(files))
	}

	// Should be sorted.
	if files[0] > files[1] {
		t.Errorf("files not sorted: %q > %q", files[0], files[1])
	}

	// All should be session_*.json paths.
	for _, f := range files {
		base := filepath.Base(f)
		if !strings.HasPrefix(base, "session_") {
			t.Errorf("unexpected file: %q", f)
		}
		if !strings.HasSuffix(base, ".json") {
			t.Errorf("unexpected file (not .json): %q", f)
		}
	}
}

// ---------------------------------------------------------------------------
// 11. WalkSessions_MissingDir
// ---------------------------------------------------------------------------

func TestSessionIndex_WalkSessions_MissingDir(t *testing.T) {
	files, err := WalkSessions("/nonexistent/directory/xyz")
	if err != nil {
		t.Fatalf("WalkSessions on missing dir returned error: %v", err)
	}
	if files == nil {
		// nil is acceptable for "no files found".
		return
	}
	if len(files) != 0 {
		t.Errorf("expected empty result, got %d files", len(files))
	}
}

// ---------------------------------------------------------------------------
// 12. TextConcatenation — mixed roles, newline separation, lowercasing
// ---------------------------------------------------------------------------

func TestSessionIndex_TextConcatenation(t *testing.T) {
	tmp := t.TempDir()
	s := sessionJSON{
		Name: "Mixed Roles",
		Messages: []messageRef{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Greetings"},
			{Role: "tool", Content: "Tool output that should be excluded"},
			{Role: "user", Content: "Next question"},
			{Role: "assistant", Content: "Final answer"},
		},
	}
	writeSession(t, tmp, "mix-ses", s)

	idx, err := BuildIndex(tmp, nil)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	e := idx.Sessions["mix-ses"]

	// MessageCount: 4 (user + assistant + user + assistant; tool excluded).
	if e.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4", e.MessageCount)
	}

	expectedText := "hello\ngreetings\nnext question\nfinal answer"
	if e.Text != expectedText {
		t.Errorf("Text = %q, want %q", e.Text, expectedText)
	}

	// Verify no tool content leaked in.
	if strings.Contains(e.Text, "tool") {
		t.Error("tool content leaked into Text")
	}

	// Verify messages are separated by newlines (exactly 3 newlines for 4 messages).
	newlineCount := strings.Count(e.Text, "\n")
	if newlineCount != 3 {
		t.Errorf("expected 3 newlines, got %d", newlineCount)
	}

	// Verify all content is lowercased.
	if e.Text != strings.ToLower(e.Text) {
		t.Errorf("Text is not lowercased: %q", e.Text)
	}
}
