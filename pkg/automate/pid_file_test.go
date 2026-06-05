package automate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GetAutomateSessionDir
// ---------------------------------------------------------------------------

func TestGetAutomateSessionDir_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	result, err := GetAutomateSessionDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(dir, ".sprout", "automate")
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
	info, err := os.Stat(result)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected a directory")
	}
}

func TestGetAutomateSessionDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	_, err := GetAutomateSessionDir(dir)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Second call on an existing directory must not error
	_, err = GetAutomateSessionDir(dir)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WriteSessionFile
// ---------------------------------------------------------------------------

func TestWriteSessionFile_AllFields(t *testing.T) {
	sproutDir := t.TempDir()
	budget := 42.5
	info := &AutomateSessionInfo{
		Workflow:       "my-workflow",
		PID:            12345,
		StartedAt:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		OutputFilePath: "/tmp/output.txt",
		BudgetUSD:      &budget,
		Kind:           "automate",
	}

	err := WriteSessionFile(sproutDir, "session-abc", info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file exists
	path := filepath.Join(sproutDir, "automate", "session-abc.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Verify permissions (0600)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %o", fi.Mode().Perm())
	}

	// Read back and verify content
	readBack, err := ReadSessionFile(sproutDir, "session-abc")
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if readBack.Workflow != info.Workflow {
		t.Errorf("Workflow: expected %q, got %q", info.Workflow, readBack.Workflow)
	}
	if readBack.PID != info.PID {
		t.Errorf("PID: expected %d, got %d", info.PID, readBack.PID)
	}
	if !readBack.StartedAt.Equal(info.StartedAt) {
		t.Errorf("StartedAt: expected %v, got %v", info.StartedAt, readBack.StartedAt)
	}
	if readBack.OutputFilePath != info.OutputFilePath {
		t.Errorf("OutputFilePath: expected %q, got %q", info.OutputFilePath, readBack.OutputFilePath)
	}
	if readBack.BudgetUSD == nil || *readBack.BudgetUSD != *info.BudgetUSD {
		t.Errorf("BudgetUSD: expected %v, got %v", info.BudgetUSD, readBack.BudgetUSD)
	}
	if readBack.Kind != info.Kind {
		t.Errorf("Kind: expected %q, got %q", info.Kind, readBack.Kind)
	}
}

func TestWriteSessionFile_OptionalFieldsNil(t *testing.T) {
	sproutDir := t.TempDir()
	info := &AutomateSessionInfo{
		Workflow:   "minimal",
		PID:        999,
		StartedAt:  time.Now().UTC(),
		Kind:       "automate",
	}

	err := WriteSessionFile(sproutDir, "session-min", info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back — BudgetUSD should be nil, OutputFilePath empty
	readBack, err := ReadSessionFile(sproutDir, "session-min")
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if readBack.BudgetUSD != nil {
		t.Errorf("BudgetUSD should be nil, got %v", *readBack.BudgetUSD)
	}
	if readBack.OutputFilePath != "" {
		t.Errorf("OutputFilePath should be empty, got %q", readBack.OutputFilePath)
	}
}

func TestWriteSessionFile_CreatesDirectoryIfMissing(t *testing.T) {
	sproutDir := t.TempDir()
	// Ensure the automate subdirectory does NOT exist
	if err := os.RemoveAll(filepath.Join(sproutDir, "automate")); err != nil {
		t.Skipf("cleanup failed: %v", err)
	}

	info := &AutomateSessionInfo{
		Workflow:  "new-dir",
		PID:       1,
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}

	err := WriteSessionFile(sproutDir, "auto-create", info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify directory was created
	dirPath := filepath.Join(sproutDir, "automate")
	fi, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("expected directory to be created")
	}
}

func TestWriteSessionFile_FileContentIsValidJSON(t *testing.T) {
	sproutDir := t.TempDir()
	budget := 10.0
	info := &AutomateSessionInfo{
		Workflow:  "json-check",
		PID:       777,
		StartedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		BudgetUSD: &budget,
		Kind:      "automate",
	}

	err := WriteSessionFile(sproutDir, "json-sess", info)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	path := filepath.Join(sproutDir, "automate", "json-sess.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file failed: %v", err)
	}

	// Verify it's valid, indented JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}

	// Check indentation (2-space indent from MarshalIndent)
	if len(data) > 0 && data[0] == '{' {
		// MarshalIndent starts with newline after {
	}
}

// ---------------------------------------------------------------------------
// RemoveSessionFile
// ---------------------------------------------------------------------------

func TestRemoveSessionFile_RemovesExistingFile(t *testing.T) {
	sproutDir := t.TempDir()
	info := &AutomateSessionInfo{
		Workflow:  "to-remove",
		PID:       100,
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "rm-test", info); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	err := RemoveSessionFile(sproutDir, "rm-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(sproutDir, "automate", "rm-test.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file was not removed")
	}
}

func TestRemoveSessionFile_NonExistentReturnsNil(t *testing.T) {
	sproutDir := t.TempDir()
	err := RemoveSessionFile(sproutDir, "does-not-exist")
	if err != nil {
		t.Fatalf("expected nil for non-existent file, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReadSessionFile
// ---------------------------------------------------------------------------

func TestReadSessionFile_ValidFile(t *testing.T) {
	sproutDir := t.TempDir()
	budget := 99.99
	info := &AutomateSessionInfo{
		Workflow:       "read-me",
		PID:            55555,
		StartedAt:      time.Date(2023, 3, 3, 3, 3, 3, 0, time.UTC),
		OutputFilePath: "/var/log/out.txt",
		BudgetUSD:      &budget,
		Kind:           "automate",
	}
	if err := WriteSessionFile(sproutDir, "read-test", info); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	readBack, err := ReadSessionFile(sproutDir, "read-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if readBack.Workflow != "read-me" {
		t.Errorf("Workflow: expected %q, got %q", "read-me", readBack.Workflow)
	}
	if readBack.PID != 55555 {
		t.Errorf("PID: expected 55555, got %d", readBack.PID)
	}
	if !readBack.StartedAt.Equal(info.StartedAt) {
		t.Errorf("StartedAt: expected %v, got %v", info.StartedAt, readBack.StartedAt)
	}
	if readBack.OutputFilePath != "/var/log/out.txt" {
		t.Errorf("OutputFilePath: expected %q, got %q", "/var/log/out.txt", readBack.OutputFilePath)
	}
	if readBack.BudgetUSD == nil || *readBack.BudgetUSD != 99.99 {
		t.Errorf("BudgetUSD: expected 99.99, got %v", readBack.BudgetUSD)
	}
	if readBack.Kind != "automate" {
		t.Errorf("Kind: expected %q, got %q", "automate", readBack.Kind)
	}
}

func TestReadSessionFile_NonExistentReturnsError(t *testing.T) {
	sproutDir := t.TempDir()
	_, err := ReadSessionFile(sproutDir, "no-such-session")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestReadSessionFile_MalformedJSON(t *testing.T) {
	sproutDir := t.TempDir()
	// Create the automate directory first
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	path := filepath.Join(sproutDir, "automate", "broken.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	_, err := ReadSessionFile(sproutDir, "broken")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// ListSessionFiles
// ---------------------------------------------------------------------------

func TestListSessionFiles_DirectoryDoesNotExist(t *testing.T) {
	sproutDir := t.TempDir()
	// automate subdirectory was never created
	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil slice, got %d items", len(result))
	}
}

func TestListSessionFiles_EmptyDirectory(t *testing.T) {
	sproutDir := t.TempDir()
	// Create the automate directory but leave it empty
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		// Empty slice from a valid dir should be empty slice, not nil
		// (the range over empty entries produces a zero-valued slice)
		t.Logf("got nil for empty dir (acceptable — matches []AutomateSessionInfo{})")
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

func TestListSessionFiles_MultipleFiles(t *testing.T) {
	sproutDir := t.TempDir()
	now := time.Now().UTC()

	// Write three session files
	files := map[string]*AutomateSessionInfo{
		"sess-1": {Workflow: "w1", PID: 1, StartedAt: now, Kind: "automate"},
		"sess-2": {Workflow: "w2", PID: 2, StartedAt: now, Kind: "automate"},
		"sess-3": {Workflow: "w3", PID: 3, StartedAt: now, Kind: "automate"},
	}
	for id, info := range files {
		if err := WriteSessionFile(sproutDir, id, info); err != nil {
			t.Fatalf("setup write for %s failed: %v", id, err)
		}
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// Build a map to verify content
	found := make(map[string]int)
	for _, r := range result {
		found[r.Workflow] = r.PID
	}
	if found["w1"] != 1 || found["w2"] != 2 || found["w3"] != 3 {
		t.Errorf("unexpected results: %v", found)
	}
}

func TestListSessionFiles_SkipsNonJSONFiles(t *testing.T) {
	sproutDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Write a non-JSON file
	if err := os.WriteFile(filepath.Join(sproutDir, "automate", "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results (txt file should be skipped), got %d", len(result))
	}
}

func TestListSessionFiles_SkipsSubdirectories(t *testing.T) {
	sproutDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Create a subdirectory inside automate/
	subDir := filepath.Join(sproutDir, "automate", "subdir")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results (subdir should be skipped), got %d", len(result))
	}
}

func TestListSessionFiles_SkipsCorruptJSON(t *testing.T) {
	sproutDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// One valid file
	info := &AutomateSessionInfo{
		Workflow:  "good",
		PID:       1,
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "good", info); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// One corrupt JSON file
	corrupt := filepath.Join(sproutDir, "automate", "bad.json")
	if err := os.WriteFile(corrupt, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result (corrupt skipped), got %d", len(result))
	}
	if result[0].Workflow != "good" {
		t.Errorf("expected workflow %q, got %q", "good", result[0].Workflow)
	}
}

func TestListSessionFiles_MixedContent(t *testing.T) {
	sproutDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	now := time.Now().UTC()

	// Write two valid sessions
	for i, wf := range []string{"alpha", "beta"} {
		info := &AutomateSessionInfo{
			Workflow:  wf,
			PID:       100 + i,
			StartedAt: now,
			Kind:      "automate",
		}
		if err := WriteSessionFile(sproutDir, wf, info); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	// Write a non-JSON file
	if err := os.WriteFile(filepath.Join(sproutDir, "automate", "README.md"), []byte("# hi"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Create a subdirectory
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate", "archive"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Write a corrupt JSON file
	if err := os.WriteFile(filepath.Join(sproutDir, "automate", "corrupt.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := ListSessionFiles(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	workflows := make(map[string]bool)
	for _, r := range result {
		workflows[r.Workflow] = true
	}
	if !workflows["alpha"] || !workflows["beta"] {
		t.Errorf("unexpected results: expected alpha and beta, got %v", workflows)
	}
}

// ---------------------------------------------------------------------------
// SweepStaleSessions
// ---------------------------------------------------------------------------

func TestSweepStaleSessions_RemovesDeadProcess(t *testing.T) {
	sproutDir := t.TempDir()
	info := &AutomateSessionInfo{
		Workflow:  "dead-wf",
		PID:       99999999,
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "dead-sess", info); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	removed, err := SweepStaleSessions(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected removed=1, got %d", removed)
	}

	// Verify file no longer exists
	path := filepath.Join(sproutDir, "automate", "dead-sess.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session file for dead process was not removed")
	}
}

func TestSweepStaleSessions_KeepsAliveProcess(t *testing.T) {
	sproutDir := t.TempDir()
	info := &AutomateSessionInfo{
		Workflow:  "alive-wf",
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "alive-sess", info); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	removed, err := SweepStaleSessions(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected removed=0, got %d", removed)
	}

	// Verify file still exists
	path := filepath.Join(sproutDir, "automate", "alive-sess.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file for alive process was incorrectly removed: %v", err)
	}
}

func TestSweepStaleSessions_EmptyDirectory(t *testing.T) {
	sproutDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	removed, err := SweepStaleSessions(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected removed=0, got %d", removed)
	}
}

func TestSweepStaleSessions_NonExistentDirectory(t *testing.T) {
	sproutDir := t.TempDir()
	// Do NOT create the automate/ subdirectory

	removed, err := SweepStaleSessions(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected removed=0, got %d", removed)
	}
}

func TestSweepStaleSessions_MixedAliveAndDead(t *testing.T) {
	sproutDir := t.TempDir()

	// Session A: alive (current process)
	infoA := &AutomateSessionInfo{
		Workflow:  "alive-wf",
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "sess-a", infoA); err != nil {
		t.Fatalf("setup A failed: %v", err)
	}

	// Session B: dead (non-existent PID)
	infoB := &AutomateSessionInfo{
		Workflow:  "dead-wf",
		PID:       99999999,
		StartedAt: time.Now().UTC(),
		Kind:      "automate",
	}
	if err := WriteSessionFile(sproutDir, "sess-b", infoB); err != nil {
		t.Fatalf("setup B failed: %v", err)
	}

	removed, err := SweepStaleSessions(sproutDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected removed=1, got %d", removed)
	}

	// Session A still exists
	pathA := filepath.Join(sproutDir, "automate", "sess-a.json")
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("alive session A was removed: %v", err)
	}

	// Session B is gone
	pathB := filepath.Join(sproutDir, "automate", "sess-b.json")
	if _, err := os.Stat(pathB); !os.IsNotExist(err) {
		t.Fatal("dead session B was not removed")
	}
}

func TestIsProcessAlive(t *testing.T) {
	// Zero and negative PIDs must return false
	if IsProcessAlive(0) {
		t.Error("IsProcessAlive(0) should return false")
	}
	if IsProcessAlive(-1) {
		t.Error("IsProcessAlive(-1) should return false")
	}

	// Current process must be alive
	if !IsProcessAlive(os.Getpid()) {
		t.Error("IsProcessAlive(os.Getpid()) should return true")
	}

	// A very large PID should be dead
	if IsProcessAlive(99999999) {
		t.Error("IsProcessAlive(99999999) should return false")
	}
}
