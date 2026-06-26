package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// === normalizeSessionID ===

func TestNormalizeSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "simple_valid",
			input: "abc123",
			want:  "abc123",
		},
		{
			name:  "with_spaces",
			input: "  my-session  ",
			want:  "my-session",
		},
		{
			name:  "legacy_prefix_stripped",
			input: "session_my-id",
			want:  "my-id",
		},
		{
			name:  "legacy_prefix_with_spaces",
			input: "  session_test  ",
			want:  "test",
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only_spaces",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "only_legacy_prefix",
			input:   "session_",
			wantErr: true,
		},
		{
			name:    "path_separator_slash",
			input:   "my/session",
			wantErr: true,
		},
		{
			name:    "path_separator_dot_dot",
			input:   "my/../session",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeSessionID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("normalizeSessionID(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// === normalizeWorkingDirectory ===

func TestNormalizeWorkingDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "empty_resolves_cwd",
			input: "",
		},
		{
			name:  "spaces_resolves_cwd",
			input: "   ",
		},
		{
			name:  "relative_path",
			input: "./subdir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeWorkingDirectory(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("result should be absolute path, got %q", got)
			}
		})
	}
}

func TestNormalizeWorkingDirectoryAbsPath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	got, err := normalizeWorkingDirectory(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the symlink-resolved cleaned absolute path.
	// On macOS, /var → /private/var so t.TempDir()'s path gets resolved.
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != resolved {
		t.Errorf("normalizeWorkingDirectory(%q) = %q; want %q", tmp, got, resolved)
	}
}

// === workingDirectoryScopeHash ===

func TestWorkingDirectoryScopeHash(t *testing.T) {
	t.Parallel()

	// Deterministic
	h1 := workingDirectoryScopeHash("/home/user/project")
	h2 := workingDirectoryScopeHash("/home/user/project")
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}

	// Case insensitive
	h1 = workingDirectoryScopeHash("/Home/User/Project")
	h2 = workingDirectoryScopeHash("/home/user/project")
	if h1 != h2 {
		t.Errorf("case-insensitive: %q != %q", h1, h2)
	}

	// Whitespace trimming
	h1 = workingDirectoryScopeHash("  /home/user  ")
	h2 = workingDirectoryScopeHash("/home/user")
	if h1 != h2 {
		t.Errorf("whitespace-trimmed: %q != %q", h1, h2)
	}

	// Different inputs produce different hashes
	h1 = workingDirectoryScopeHash("/home/user/a")
	h2 = workingDirectoryScopeHash("/home/user/b")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}

	// Output is 16 hex chars (8 bytes)
	h := workingDirectoryScopeHash("test")
	if len(h) != 16 {
		t.Errorf("hash length = %d; want 16", len(h))
	}
}

// === buildScopedSessionFilePath ===

func TestBuildScopedSessionFilePath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path, err := buildScopedSessionFilePath(tmp, "my-session", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have structure: tmp/scoped/<8-char-hex>/session_my-session.json
	// Check it contains the scoped directory
	if !filepath.HasPrefix(filepath.Dir(path), filepath.Join(tmp, "scoped")) {
		t.Errorf("path should be under scoped dir: %s", path)
	}
	if filepath.Base(path) != "session_my-session.json" {
		t.Errorf("basename = %q; want 'session_my-session.json'", filepath.Base(path))
	}
}

func TestBuildScopedSessionFilePathInvalidSessionID(t *testing.T) {
	t.Parallel()

	_, err := buildScopedSessionFilePath("/tmp", "", "/home")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}

	_, err = buildScopedSessionFilePath("/tmp", "bad/path", "/home")
	if err == nil {
		t.Fatal("expected error for session ID with path separator")
	}
}

func TestBuildScopedSessionFilePathInvalidWorkingDir(t *testing.T) {
	t.Parallel()

	_, err := buildScopedSessionFilePath("/tmp", "ok", "")
	// Empty working dir should resolve to cwd, so it should succeed
	if err != nil {
		t.Fatalf("empty working dir should resolve to cwd: %v", err)
	}
}

// === resolveSessionStateFile ===

func TestResolveSessionStateFileScopedFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Create a scoped session file
	scopeHash := workingDirectoryScopeHash("/home/user/project")
	scopeDir := filepath.Join(tmp, "scoped", scopeHash)
	if err := os.MkdirAll(scopeDir, 0700); err != nil {
		t.Fatalf("failed to create scope dir: %v", err)
	}
	sessionFile := filepath.Join(scopeDir, "session_my-session.json")
	if err := os.WriteFile(sessionFile, []byte("{}"), 0600); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	// resolveSessionStateFile takes stateDir as first arg — no global patching needed.
	got, err := resolveSessionStateFile(tmp, "my-session", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sessionFile {
		t.Errorf("got %q; want %q", got, sessionFile)
	}
}

func TestResolveSessionStateFileLegacyFallback(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	legacyFile := filepath.Join(tmp, "session_legacy.json")
	if err := os.WriteFile(legacyFile, []byte("{}"), 0600); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	got, err := resolveSessionStateFile(tmp, "legacy", "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != legacyFile {
		t.Errorf("got %q; want %q", got, legacyFile)
	}
}

func TestResolveSessionStateFileNotFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	_, err := resolveSessionStateFile(tmp, "no-such-session", "/some/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("wrong error message: %v", err)
	}
}

func TestResolveSessionStateFileAmbiguous(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Create two scoped directories with same session name
	for i := 0; i < 2; i++ {
		scopeDir := filepath.Join(tmp, "scoped", "hash000000000000000"+string(rune('0'+i)))
		if err := os.MkdirAll(scopeDir, 0700); err != nil {
			t.Fatalf("failed to create scope dir %d: %v", i, err)
		}
		sessionFile := filepath.Join(scopeDir, "session_dup.json")
		if err := os.WriteFile(sessionFile, []byte("{}"), 0600); err != nil {
			t.Fatalf("failed to write session file: %v", err)
		}
	}

	_, err := resolveSessionStateFile(tmp, "dup", "/some/dir")
	if err == nil {
		t.Fatal("expected ambiguous error for multiple scoped matches")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("wrong error message: %v", err)
	}
}

// === ExportStateToJSON / ImportStateFromJSONFile ===

func TestExportStateToJSON(t *testing.T) {
	t.Parallel()

	state := &ConversationState{
		SessionID:   "test-session",
		Name:        "Test",
		TotalCost:   1.5,
		TotalTokens: 1000,
	}

	data, err := ExportStateToJSON(state)
	if err != nil {
		t.Fatalf("ExportStateToJSON failed: %v", err)
	}

	// Should produce valid JSON containing our fields
	if !strings.Contains(string(data), `"session_id"`) {
		t.Error("JSON should contain session_id")
	}
	if !strings.Contains(string(data), `"name"`) {
		t.Error("JSON should contain name")
	}
	if !strings.Contains(string(data), "test-session") {
		t.Error("JSON should contain session value")
	}
}

func TestImportStateFromJSONFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	jsonFile := filepath.Join(tmp, "state.json")

	// Write valid JSON
	jsonData := `{
		"session_id": "imported",
		"name": "Imported Session",
		"total_cost": 5.0,
		"total_tokens": 5000,
		"working_directory": "/some/dir"
	}`
	if err := os.WriteFile(jsonFile, []byte(jsonData), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	state, err := ImportStateFromJSONFile(jsonFile)
	if err != nil {
		t.Fatalf("ImportStateFromJSONFile failed: %v", err)
	}

	if state.SessionID != "imported" {
		t.Errorf("SessionID = %q; want 'imported'", state.SessionID)
	}
	if state.Name != "Imported Session" {
		t.Errorf("Name = %q; want 'Imported Session'", state.Name)
	}
	if state.TotalCost != 5.0 {
		t.Errorf("TotalCost = %f; want 5.0", state.TotalCost)
	}
	if state.TotalTokens != 5000 {
		t.Errorf("TotalTokens = %d; want 5000", state.TotalTokens)
	}
	if state.WorkingDirectory != "/some/dir" {
		t.Errorf("WorkingDirectory = %q; want '/some/dir'", state.WorkingDirectory)
	}
}

func TestImportStateFromJSONFileMissing(t *testing.T) {
	t.Parallel()

	_, err := ImportStateFromJSONFile("/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestImportStateFromJSONFileInvalidJSON(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	jsonFile := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(jsonFile, []byte("not json {{{"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := ImportStateFromJSONFile(jsonFile)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
