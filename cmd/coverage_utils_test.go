package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// FormatDuration
// =============================================================================

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0.0s"},
		{"milliseconds", 500 * time.Millisecond, "0.5s"},
		{"30 seconds", 30 * time.Second, "30.0s"},
		{"59 seconds", 59 * time.Second, "59.0s"},
		{"1 minute", 1 * time.Minute, "1.0m"},
		{"1.5 minutes", 90 * time.Second, "1.5m"},
		{"30 minutes", 30 * time.Minute, "30.0m"},
		{"59 minutes", 59 * time.Minute, "59.0m"},
		{"1 hour", 1 * time.Hour, "1.0h"},
		{"2.5 hours", 150 * time.Minute, "2.5h"},
		{"10 hours", 10 * time.Hour, "10.0h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.d)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// =============================================================================
// GetTerminalWidth
// =============================================================================

func TestGetTerminalWidth(t *testing.T) {
	width := GetTerminalWidth()
	if width <= 0 {
		t.Errorf("GetTerminalWidth() returned %d, expected a positive int", width)
	}
	// The fallback is 78, so it should be at least 40 (minimum cap)
	if width < 40 {
		t.Errorf("GetTerminalWidth() returned %d, expected >= 40", width)
	}
	// Maximum cap is 200
	if width > 200 {
		t.Errorf("GetTerminalWidth() returned %d, expected <= 200", width)
	}
}

// =============================================================================
// IsCI
// =============================================================================

func TestIsCI_NoCIEnv(t *testing.T) {
	// Ensure no CI env vars are set
	origCI := os.Getenv("CI")
	origGA := os.Getenv("GITHUB_ACTIONS")
	os.Unsetenv("CI")
	os.Unsetenv("GITHUB_ACTIONS")
	defer func() {
		if origCI != "" {
			os.Setenv("CI", origCI)
		} else {
			os.Unsetenv("CI")
		}
		if origGA != "" {
			os.Setenv("GITHUB_ACTIONS", origGA)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
	}()

	if IsCI() {
		t.Error("IsCI() should return false when no CI env vars are set")
	}
}

func TestIsCI_WithCI(t *testing.T) {
	origCI := os.Getenv("CI")
	os.Setenv("CI", "true")
	defer func() {
		if origCI != "" {
			os.Setenv("CI", origCI)
		} else {
			os.Unsetenv("CI")
		}
	}()

	if !IsCI() {
		t.Error("IsCI() should return true when CI is set")
	}
}

func TestIsCI_WithGitHubActions(t *testing.T) {
	origCI := os.Getenv("CI")
	origGA := os.Getenv("GITHUB_ACTIONS")
	os.Unsetenv("CI") // Ensure CI doesn't mask the GITHUB_ACTIONS path
	os.Setenv("GITHUB_ACTIONS", "true")
	defer func() {
		if origCI != "" {
			os.Setenv("CI", origCI)
		} else {
			os.Unsetenv("CI")
		}
		if origGA != "" {
			os.Setenv("GITHUB_ACTIONS", origGA)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
	}()

	if !IsCI() {
		t.Error("IsCI() should return true when GITHUB_ACTIONS is set")
	}
}

// =============================================================================
// enhanceCommandForColors
// =============================================================================

func TestEnhanceCommandForColors(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "git status",
			cmd:  "git status",
			want: "git -c color.ui=always status",
		},
		{
			name: "git log with args",
			cmd:  "git log --oneline -10",
			want: "git -c color.ui=always log --oneline -10",
		},
		{
			name: "git diff",
			cmd:  "git diff HEAD~1",
			want: "git -c color.ui=always diff HEAD~1",
		},
		{
			name: "git alone (no subcommand)",
			cmd:  "git",
			want: "git",
		},
		{
			name: "ls command",
			cmd:  "ls -la",
			want: "ls --color=auto -la",
		},
		{
			name: "ls already has color",
			cmd:  "ls --color=always",
			want: "ls --color=always",
		},
		{
			name: "grep command",
			cmd:  "grep -r pattern .",
			want: "grep --color=auto -r pattern .",
		},
		{
			name: "grep already has color",
			cmd:  "grep --color=auto foo",
			want: "grep --color=auto foo",
		},
		{
			name: "unknown command passthrough",
			cmd:  "echo hello",
			want: "echo hello",
		},
		{
			name: "python command passthrough",
			cmd:  "python -m pytest",
			want: "python -m pytest",
		},
		{
			name: "whitespace trimmed input",
			cmd:  "  git status  ",
			want: "git -c color.ui=always status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enhanceCommandForColors(tt.cmd)
			if got != tt.want {
				t.Errorf("enhanceCommandForColors(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// itoa
// =============================================================================

func TestItoa(t *testing.T) {
	tests := []struct {
		name string
		v    int
		want string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"nine", 9, "9"},
		{"ten", 10, "10"},
		{"hundred", 100, "100"},
		{"thousand", 1000, "1000"},
		{"million", 1000000, "1000000"},
		{"negative one", -1, "-1"},
		{"negative hundred", -100, "-100"},
		{"negative million", -1000000, "-1000000"},
		{"max int32", 2147483647, "2147483647"},
		{"min int32", -2147483648, "-2147483648"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itoa(tt.v)
			if got != tt.want {
				t.Errorf("itoa(%d) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

// =============================================================================
// printVersionInfo
// =============================================================================

func TestPrintVersionInfo(t *testing.T) {
	out := captureStdout(t, printVersionInfo)

	// Should always contain these strings
	if !strings.Contains(out, "ledit version") {
		t.Errorf("printVersionInfo() output missing 'ledit version', got:\n%s", out)
	}
	if !strings.Contains(out, "Go version") {
		t.Errorf("printVersionInfo() output missing 'Go version', got:\n%s", out)
	}
	if !strings.Contains(out, "Platform") {
		t.Errorf("printVersionInfo() output missing 'Platform', got:\n%s", out)
	}
	// Should contain a module path (from debug.ReadBuildInfo)
	if !strings.Contains(out, "Module") {
		t.Errorf("printVersionInfo() output missing 'Module', got:\n%s", out)
	}
}

// =============================================================================
// extractDescription
// =============================================================================

func TestExtractDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "valid YAML front matter with description",
			content: "---\ndescription: A helpful skill for testing\n---\nSome content here",
			want:    "A helpful skill for testing",
		},
		{
			name:    "description with leading/trailing whitespace",
			content: "---\ndescription:   spaced out  \n---\nContent",
			want:    "spaced out",
		},
		{
			name:    "description with colon value",
			content: "---\ndescription: Use colon: like this\n---\nBody",
			want:    "Use colon: like this",
		},
		{
			name:    "no front matter",
			content: "Just some plain text content\nwith no YAML front matter",
			want:    "(no description)",
		},
		{
			name:    "empty content",
			content: "",
			want:    "(no description)",
		},
		{
			name:    "front matter missing description key",
			content: "---\nname: my-skill\nauthor: dev\n---\nBody",
			want:    "(no description)",
		},
		{
			name:    "empty description value",
			content: "---\ndescription:\n---\nBody",
			want:    "",
		},
		{
			name:    "description not at start of line",
			content: "---\n  notdescription: something\ndescription: correct\n---\nBody",
			want:    "correct",
		},
		{
			name:    "only opening front matter delimiter",
			content: "---\ndescription: incomplete front matter",
			want:    "incomplete front matter",
		},
		{
			name:    "description before front matter ignored",
			content: "description: before front matter\n---\ndescription: real one\n---\nBody",
			want:    "real one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDescription(tt.content)
			if got != tt.want {
				t.Errorf("extractDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// getConfigDir
// =============================================================================

func TestGetConfigDir_LEDIT_CONFIG(t *testing.T) {
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", "/custom/config/path")
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	got := getConfigDir()
	if got != "/custom/config/path" {
		t.Errorf("getConfigDir() with LEDIT_CONFIG = %q, want %q", got, "/custom/config/path")
	}
}

func TestGetConfigDir_XDG_CONFIG_HOME(t *testing.T) {
	origLedit := os.Getenv("LEDIT_CONFIG")
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Unsetenv("LEDIT_CONFIG")
	os.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	defer func() {
		if origLedit != "" {
			os.Setenv("LEDIT_CONFIG", origLedit)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	got := getConfigDir()
	want := filepath.Join("/custom/xdg", "ledit")
	if got != want {
		t.Errorf("getConfigDir() with XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}

func TestGetConfigDir_HOME(t *testing.T) {
	origLedit := os.Getenv("LEDIT_CONFIG")
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	origHome := os.Getenv("HOME")
	os.Unsetenv("LEDIT_CONFIG")
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Setenv("HOME", "/home/testuser")
	defer func() {
		if origLedit != "" {
			os.Setenv("LEDIT_CONFIG", origLedit)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	got := getConfigDir()
	want := filepath.Join("/home/testuser", ".ledit")
	if got != want {
		t.Errorf("getConfigDir() with HOME = %q, want %q", got, want)
	}
}

func TestGetConfigDir_Fallback(t *testing.T) {
	origLedit := os.Getenv("LEDIT_CONFIG")
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	origHome := os.Getenv("HOME")
	os.Unsetenv("LEDIT_CONFIG")
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("HOME")
	defer func() {
		if origLedit != "" {
			os.Setenv("LEDIT_CONFIG", origLedit)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	got := getConfigDir()
	want := "/data/data/com.termux/files/home/.ledit"
	if got != want {
		t.Errorf("getConfigDir() fallback = %q, want %q", got, want)
	}
}

// =============================================================================
// loadInstances
// =============================================================================

func TestLoadInstances_FileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	instances, err := loadInstances()
	if err != nil {
		t.Fatalf("loadInstances() unexpected error: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected empty map, got %d entries", len(instances))
	}
}

func TestLoadInstances_ValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	validJSON := `{
  "instance_1": {
    "id": "instance_1",
    "port": 8080,
    "pid": 12345,
    "start_time": "2024-01-01T00:00:00Z",
    "working_dir": "/home/user/project",
    "last_ping": "2024-01-01T00:01:00Z",
    "session_id": "sess_abc"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "instances.json"), []byte(validJSON), 0644); err != nil {
		t.Fatal(err)
	}

	instances, err := loadInstances()
	if err != nil {
		t.Fatalf("loadInstances() unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	info, ok := instances["instance_1"]
	if !ok {
		t.Fatal("expected instance_1 in map")
	}
	if info.Port != 8080 {
		t.Errorf("expected port 8080, got %d", info.Port)
	}
	if info.PID != 12345 {
		t.Errorf("expected pid 12345, got %d", info.PID)
	}
	if info.SessionID != "sess_abc" {
		t.Errorf("expected session_id sess_abc, got %q", info.SessionID)
	}
}

func TestLoadInstances_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	if err := os.WriteFile(filepath.Join(tmpDir, "instances.json"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	instances, err := loadInstances()
	if err != nil {
		t.Fatalf("loadInstances() unexpected error: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected empty map, got %d entries", len(instances))
	}
}

func TestLoadInstances_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	if err := os.WriteFile(filepath.Join(tmpDir, "instances.json"), []byte("not valid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadInstances()
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// =============================================================================
// saveInstances
// =============================================================================

func TestSaveInstances(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	instances := map[string]InstanceInfo{
		"inst_1": {
			ID:         "inst_1",
			Port:       9090,
			PID:        9999,
			WorkingDir: "/tmp/test",
			LastPing:   time.Now(),
		},
		"inst_2": {
			ID:         "inst_2",
			Port:       9091,
			PID:        9998,
			WorkingDir: "/tmp/test2",
			LastPing:   time.Now(),
		},
	}

	err := saveInstances(instances)
	if err != nil {
		t.Fatalf("saveInstances() error: %v", err)
	}

	// Verify the file was created
	filePath := filepath.Join(tmpDir, "instances.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read instances.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "inst_1") {
		t.Error(" instances.json missing inst_1")
	}
	if !strings.Contains(content, "inst_2") {
		t.Error("instances.json missing inst_2")
	}
	if !strings.Contains(content, "9090") {
		t.Error("instances.json missing port 9090")
	}

	// Verify we can load it back
	loaded, err := loadInstances()
	if err != nil {
		t.Fatalf("failed to reload instances: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 loaded instances, got %d", len(loaded))
	}
}

func TestSaveInstances_EmptyMap(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	err := saveInstances(map[string]InstanceInfo{})
	if err != nil {
		t.Fatalf("saveInstances() empty map error: %v", err)
	}

	loaded, err := loadInstances()
	if err != nil {
		t.Fatalf("failed to reload instances: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 loaded instances, got %d", len(loaded))
	}
}

func TestSaveInstances_CreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "sub", "nested")
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", nestedDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	instances := map[string]InstanceInfo{
		"inst_1": {ID: "inst_1", Port: 8080},
	}

	err := saveInstances(instances)
	if err != nil {
		t.Fatalf("saveInstances() error: %v", err)
	}

	filePath := filepath.Join(nestedDir, "instances.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("instances.json was not created in nested directory")
	}
}

// =============================================================================
// cleanStaleInstances
// =============================================================================

func TestCleanStaleInstances(t *testing.T) {
	now := time.Now()
	staleThreshold := now.Add(-10 * time.Second)

	instances := map[string]InstanceInfo{
		"fresh_1": {
			ID:       "fresh_1",
			LastPing: now, // recent ping, should NOT be removed
		},
		"fresh_2": {
			ID:       "fresh_2",
			LastPing: now.Add(-1 * time.Second), // 1 sec ago, should NOT be removed
		},
		"stale_1": {
			ID:       "stale_1",
			LastPing: now.Add(-30 * time.Second), // 30 sec ago, should be removed
		},
		"stale_2": {
			ID:       "stale_2",
			LastPing: now.Add(-1 * time.Minute), // 1 min ago, should be removed
		},
		"boundary": {
			ID:       "boundary",
			LastPing: staleThreshold.Add(-1 * time.Nanosecond), // 1ns before threshold, should be removed
		},
	}

	cleanStaleInstances(instances, staleThreshold)

	if len(instances) != 2 {
		t.Errorf("expected 2 instances after cleanup, got %d", len(instances))
	}
	if _, ok := instances["fresh_1"]; !ok {
		t.Error("fresh_1 should not have been removed")
	}
	if _, ok := instances["fresh_2"]; !ok {
		t.Error("fresh_2 should not have been removed")
	}
	if _, ok := instances["stale_1"]; ok {
		t.Error("stale_1 should have been removed")
	}
	if _, ok := instances["stale_2"]; ok {
		t.Error("stale_2 should have been removed")
	}
	if _, ok := instances["boundary"]; ok {
		t.Error("boundary (exact threshold) should have been removed")
	}
}

func TestCleanStaleInstances_AllFresh(t *testing.T) {
	now := time.Now()
	staleThreshold := now.Add(-1 * time.Hour)

	instances := map[string]InstanceInfo{
		"inst_1": {ID: "inst_1", LastPing: now},
		"inst_2": {ID: "inst_2", LastPing: now.Add(-30 * time.Minute)},
	}

	cleanStaleInstances(instances, staleThreshold)

	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}

func TestCleanStaleInstances_AllStale(t *testing.T) {
	now := time.Now()
	staleThreshold := now.Add(-1 * time.Minute)

	instances := map[string]InstanceInfo{
		"old_1": {ID: "old_1", LastPing: now.Add(-5 * time.Minute)},
		"old_2": {ID: "old_2", LastPing: now.Add(-1 * time.Hour)},
	}

	cleanStaleInstances(instances, staleThreshold)

	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestCleanStaleInstances_EmptyMap(t *testing.T) {
	instances := map[string]InstanceInfo{}
	staleThreshold := time.Now().Add(-10 * time.Second)

	// Should not panic
	cleanStaleInstances(instances, staleThreshold)

	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestCleanStaleInstances_IntegrationWithSaveAndLoad(t *testing.T) {
	now := time.Now()
	tmpDir := t.TempDir()
	orig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if orig != "" {
			os.Setenv("LEDIT_CONFIG", orig)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	instances := map[string]InstanceInfo{
		"fresh": {ID: "fresh", Port: 8080, LastPing: now},
		"stale": {ID: "stale", Port: 8081, LastPing: now.Add(-5 * time.Minute)},
	}

	// Save with both
	if err := saveInstances(instances); err != nil {
		t.Fatalf("saveInstances() error: %v", err)
	}

	// Load and clean stale
	loaded, err := loadInstances()
	if err != nil {
		t.Fatalf("loadInstances() error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 instances loaded, got %d", len(loaded))
	}

	cleanStaleInstances(loaded, now.Add(-1*time.Minute))

	if len(loaded) != 1 {
		t.Errorf("expected 1 instance after cleanup, got %d", len(loaded))
	}
	if _, ok := loaded["fresh"]; !ok {
		t.Error("fresh instance should remain")
	}
	if _, ok := loaded["stale"]; ok {
		t.Error("stale instance should be removed")
	}

	// Save cleaned and verify persistence
	if err := saveInstances(loaded); err != nil {
		t.Fatalf("saveInstances() after cleanup error: %v", err)
	}

	final, err := loadInstances()
	if err != nil {
		t.Fatalf("loadInstances() after cleanup error: %v", err)
	}
	if len(final) != 1 || final["fresh"].Port != 8080 {
		t.Errorf("persistence mismatch: got %v", final)
	}
}
