//go:build !js

package workflow

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// logCaptureMu serializes log.SetOutput calls across parallel tests to prevent
// race conditions on the global log.Writer().
var logCaptureMu sync.Mutex

// TestAllowedPath_Validate exercises every rule enforced by
// AllowedPath.Validate() (SP-128-1a). The test mirrors the spec's
// B3 validation rules; a regression in any of them causes a workflow
// author to see a confusing error mid-workflow run instead of a
// clear schema-level rejection at load time.
func TestAllowedPath_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		mode    string
		wantErr string // substring; empty == expect nil error
	}{
		// — valid —
		{"absolute read_write accepted", "/srv/datasets", "read_write", ""},
		{"absolute read_only accepted", "/var/log/sprout", "read_only", ""},
		{"deep path accepted", "/srv/datasets/2024/q4.parquet", "read_write", ""},
		{"trailing slash rejected (needs clean)", "/srv/data/", "read_write", "must already be cleaned"},

		// — relative path rejected —
		{"relative path rejected", "srv/datasets", "read_write", "path must be absolute"},
		{"dot relative rejected", "./srv/datasets", "read_write", "path must be absolute"},

		// — traversal rejected —
		{"dotdot rejected", "/foo/../etc", "read_write", "must already be cleaned"},
		{"embedded dotdot rejected", "/foo/../etc/passwd", "read_write", "must already be cleaned"},
		{"leading dotdot rejected", "../etc", "read_write", "path must be absolute"},

		// — tilde rejected —
		{"tilde rejected", "~/datasets", "read_write", "must not start with `~`"},
		{"tilde under absolute rejected", "/~/datasets", "read_write", ""}, // absolute still passes — only the leading ~ is rejected
		{"only tilde rejected", "~", "read_write", "must not start with `~`"},

		// — empty path rejected —
		{"empty path rejected", "", "read_write", "path is required"},
		{"whitespace path rejected", "   ", "read_write", "path is required"},

		// — mode validation —
		{"empty mode rejected", "/srv/data", "", "mode must be"},
		{"uppercase mode rejected", "/srv/data", "READ_WRITE", "mode must be"},
		{"mixed-case mode rejected", "/srv/data", "Read_Only", "mode must be"},
		{"rw abbreviation rejected", "/srv/data", "rw", "mode must be"},
		{"RO abbreviation rejected", "/srv/data", "RO", "mode must be"},
		{"invalid keyword rejected", "/srv/data", "deny", "mode must be"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ap := AllowedPath{Path: tt.path, Mode: tt.mode}
			err := ap.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestAgentWorkflowConfig_Validate_AllowedPaths drives the loader's
// allowed_paths validation through the public LoadAgentWorkflowConfig
// entry point. It checks that an entry with a bad mode causes the
// whole workflow to fail to load — i.e. the schema check runs BEFORE
// the rest of Validate, satisfying the spec's "fail fast" requirement.
func TestAgentWorkflowConfig_Validate_AllowedPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "relative/path", "mode": "read_write"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for relative path; got nil")
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify offending index, got: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should mention 'absolute', got: %v", err)
	}
}

// TestAgentWorkflowConfig_Validate_AllowedPaths_BadMode checks the
// mode-enum check fires when an entry uses a wrong value.
func TestAgentWorkflowConfig_Validate_AllowedPaths_BadMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "rw"},
			{"path": "/var/log/sprout", "mode": "RO"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for invalid mode; got nil")
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify offending index 0, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("error should mention 'mode must be', got: %v", err)
	}
}

// TestAgentWorkflowConfig_Validate_AllowedPaths_Traversal verifies
// the loader rejects paths containing `..` segments after Clean —
// the rule is in B3 and is what would otherwise let a workflow
// author slip `/foo/../etc` past the gate.
func TestAgentWorkflowConfig_Validate_AllowedPaths_Traversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "x"},
		"allowed_paths": [
			{"path": "/srv/data/../etc", "mode": "read_only"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for traversal path; got nil")
	}
	if !strings.Contains(err.Error(), "must already be cleaned") {
		t.Fatalf("error should mention cleaning rule, got: %v", err)
	}
}

// TestAgentWorkflowConfig_Validate_AllowedPaths_AbsoluteAccepted
// sanity-checks the happy path: a clean absolute path with a valid
// mode loads successfully and the trimmed values land on the
// config struct.
func TestAgentWorkflowConfig_Validate_AllowedPaths_AbsoluteAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "x"},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "read_write", "reason": "Read training data"},
			{"path": "/var/log/sprout", "mode": "read_only"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success; got: %v", err)
	}
	if len(cfg.AllowedPaths) != 2 {
		t.Fatalf("expected 2 allowed_paths entries, got %d", len(cfg.AllowedPaths))
	}
	if cfg.AllowedPaths[0].Path != "/srv/datasets" || cfg.AllowedPaths[0].Mode != "read_write" || cfg.AllowedPaths[0].Reason != "Read training data" {
		t.Errorf("entry[0] not preserved: %+v", cfg.AllowedPaths[0])
	}
	if cfg.AllowedPaths[1].Mode != "read_only" {
		t.Errorf("entry[1].Mode = %q; want read_only", cfg.AllowedPaths[1].Mode)
	}
}

// TestAgentWorkflowConfig_Validate_AllowedPaths_SystemPrefixWarn
// verifies that a path under /etc (or any other system prefix) is
// allowed to load (warning, not error), and that the loader emits
// a log line so the user can see the heads-up. We capture the
// logger output via a redirect rather than asserting on the exact
// text — the wording is advisory and may evolve.
func TestAgentWorkflowConfig_Validate_AllowedPaths_SystemPrefixWarn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "x"},
		"allowed_paths": [
			{"path": "/etc/sprout-stuff", "mode": "read_only"},
			{"path": "/System/Library/foo", "mode": "read_only"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the lock for the entire test body so the buffer snapshot is stable.
	logCaptureMu.Lock()
	defer logCaptureMu.Unlock()
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected system-prefix entries to load with a warning, got error: %v", err)
	}
	if len(cfg.AllowedPaths) != 2 {
		t.Fatalf("expected 2 allowed_paths entries, got %d", len(cfg.AllowedPaths))
	}
	logs := buf.String()
	if !strings.Contains(logs, "WARNING") {
		t.Fatalf("expected a WARNING log line for the system prefix, got logs: %q", logs)
	}
	if !strings.Contains(logs, "/etc/sprout-stuff") {
		t.Fatalf("warning should mention the offending path, got logs: %q", logs)
	}
}

// TestAllowedPath_NilReceiver guards against accidental nil-pointer
// panics on the validation path. The loader iterates c.AllowedPaths
// and calls ap.Validate(); a nil entry in the slice (e.g. JSON
// `null`) must be a no-op rather than a panic.
func TestAllowedPath_NilReceiver(t *testing.T) {
	t.Parallel()
	var ap *AllowedPath
	if err := ap.Validate(); err != nil {
		t.Fatalf("nil Validate() should be a no-op, got: %v", err)
	}
}

// TestIsSystemPathPrefix exercises the system-prefix detection with
// every entry on the list — guards against typos in the prefix
// strings, since a regression here would silently let workflows
// touch /etc without a warning.
func TestIsSystemPathPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"/etc", true},
		{"/etc/passwd", true},
		{"/usr/local/bin", true},
		{"/var/log/sprout", true},
		{"/bin/sh", true},
		{"/sbin/reboot", true},
		{"/boot/vmlinuz", true},
		{"/proc/cpuinfo", true},
		{"/sys/devices", true},
		{"/dev/null", true},
		{"/lib/x86_64-linux-gnu", true},
		{"/lib64/foo", true},
		{"/opt/app", true},
		{"/root/.ssh", true},
		{"/System/Library/foo", true},
		{"/Library/Frameworks", true},
		{"/private/etc/passwd", true},
		{"/private/var/log", true},
		{"/Applications/Safari.app", true},

		// Not a system prefix — false positives would be bad.
		{"/srv/datasets", false},
		{"/home/user/projects", false},
		{"/tmp/foo", false},
		{"/etcetera", false}, // /etc is a prefix but /etcetera is NOT under /etc (component boundary)
		{"", false},
	}
	for _, c := range cases {
		got := IsSystemPathPrefix(c.path)
		if got != c.want {
			t.Errorf("IsSystemPathPrefix(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// SP-127 Phase 2: Step-level allowed_paths validation
// ---------------------------------------------------------------------------

// TestValidate_StepAllowedPaths_ValidEntry verifies that a valid step-level
// allowed_path passes validation.
func TestValidate_StepAllowedPaths_ValidEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "/srv/datasets", "mode": "read_only", "reason": "Read training data"},
					{"path": "/tmp/output", "mode": "read_write"}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success; got: %v", err)
	}
	if len(cfg.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(cfg.Steps))
	}
	if len(cfg.Steps[0].AllowedPaths) != 2 {
		t.Fatalf("expected 2 allowed_paths entries on step, got %d", len(cfg.Steps[0].AllowedPaths))
	}
	if cfg.Steps[0].AllowedPaths[0].Path != "/srv/datasets" || cfg.Steps[0].AllowedPaths[0].Mode != "read_only" {
		t.Errorf("step allowed_paths[0] wrong: %+v", cfg.Steps[0].AllowedPaths[0])
	}
	if cfg.Steps[0].AllowedPaths[1].Path != "/tmp/output" || cfg.Steps[0].AllowedPaths[1].Mode != "read_write" {
		t.Errorf("step allowed_paths[1] wrong: %+v", cfg.Steps[0].AllowedPaths[1])
	}
}

// TestValidate_StepAllowedPaths_InvalidRelativePath verifies that an invalid
// step-level allowed_path (relative path) returns an error.
func TestValidate_StepAllowedPaths_InvalidRelativePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "relative/path", "mode": "read_write"}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for relative path in step-level allowed_paths; got nil")
	}
	if !strings.Contains(err.Error(), "steps[0]") {
		t.Fatalf("error should identify steps[0] scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify allowed_paths[0] index, got: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should mention 'absolute', got: %v", err)
	}
}

// TestValidate_StepAllowedPaths_InvalidMode verifies that an invalid mode
// on a step-level allowed_path returns an error.
func TestValidate_StepAllowedPaths_InvalidMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "/srv/datasets", "mode": "rw"}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for invalid mode in step-level allowed_paths; got nil")
	}
	if !strings.Contains(err.Error(), "steps[0]") {
		t.Fatalf("error should identify steps[0] scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify allowed_paths[0] index, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("error should mention 'mode must be', got: %v", err)
	}
}

// TestValidate_StepAllowedPaths_WorkflowStepConflictDifferentModes verifies that
// when the same path is declared at both workflow-level and step-level with
// different modes, the workflow loads successfully (step-level mode wins) and
// a warning is logged.
func TestValidate_StepAllowedPaths_WorkflowStepConflictDifferentModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "read_only"}
		],
		"steps": [
			{
				"prompt": "process data",
				"allowed_paths": [
					{"path": "/srv/datasets", "mode": "read_write"}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the lock for the entire test body so the buffer snapshot is stable.
	logCaptureMu.Lock()
	defer logCaptureMu.Unlock()
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success with conflicting modes; got: %v", err)
	}
	// Workflow-level path is preserved as-is.
	if len(cfg.AllowedPaths) != 1 {
		t.Fatalf("expected 1 workflow-level allowed_path, got %d", len(cfg.AllowedPaths))
	}
	// Step-level path is also preserved.
	if len(cfg.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(cfg.Steps))
	}
	if len(cfg.Steps[0].AllowedPaths) != 1 {
		t.Fatalf("expected 1 step-level allowed_path, got %d", len(cfg.Steps[0].AllowedPaths))
	}
	// Check warning was logged.
	logs := buf.String()
	if !strings.Contains(logs, "WARNING") {
		t.Fatalf("expected a WARNING log line for the mode conflict, got logs: %q", logs)
	}
	if !strings.Contains(logs, "/srv/datasets") {
		t.Fatalf("warning should mention the conflicting path, got logs: %q", logs)
	}
	if !strings.Contains(logs, "workflow level") {
		t.Fatalf("warning should mention 'workflow level', got logs: %q", logs)
	}
	if !strings.Contains(logs, "steps[0]") {
		t.Fatalf("warning should mention 'steps[0]', got logs: %q", logs)
	}
}

// TestValidate_InitialAllowedPaths_ValidEntry verifies that a valid initial-level
// allowed_path passes validation.
func TestValidate_InitialAllowedPaths_ValidEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "/tmp/work", "mode": "read_write", "reason": "Temp workspace"}
			]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success; got: %v", err)
	}
	if cfg.Initial == nil {
		t.Fatal("expected Initial to be non-nil")
	}
	if len(cfg.Initial.AllowedPaths) != 1 {
		t.Fatalf("expected 1 initial-level allowed_path, got %d", len(cfg.Initial.AllowedPaths))
	}
	if cfg.Initial.AllowedPaths[0].Path != "/tmp/work" || cfg.Initial.AllowedPaths[0].Mode != "read_write" {
		t.Errorf("initial allowed_paths[0] wrong: %+v", cfg.Initial.AllowedPaths[0])
	}
}

// TestValidate_InitialAllowedPaths_InvalidEntry verifies that an invalid
// initial-level allowed_path returns an error.
func TestValidate_InitialAllowedPaths_InvalidEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "relative/path", "mode": "read_write"}
			]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected load error for relative path in initial-level allowed_paths; got nil")
	}
	if !strings.Contains(err.Error(), "initial") {
		t.Fatalf("error should identify 'initial' scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowed_paths[0]") {
		t.Fatalf("error should identify allowed_paths[0] index, got: %v", err)
	}
}

// TestValidate_InitialAllowedPaths_SystemPrefixWarn verifies that an initial-level
// allowed_path under a system prefix is allowed to load with a warning.
func TestValidate_InitialAllowedPaths_SystemPrefixWarn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "/etc/sprout-stuff", "mode": "read_only"}
			]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the lock for the entire test body so the buffer snapshot is stable.
	logCaptureMu.Lock()
	defer logCaptureMu.Unlock()
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected system-prefix entry to load with a warning, got error: %v", err)
	}
	if cfg.Initial == nil || len(cfg.Initial.AllowedPaths) != 1 {
		t.Fatalf("expected 1 initial-level allowed_path, got %v", cfg.Initial)
	}
	logs := buf.String()
	if !strings.Contains(logs, "WARNING") {
		t.Fatalf("expected a WARNING log line for the system prefix, got logs: %q", logs)
	}
	if !strings.Contains(logs, "/etc/sprout-stuff") {
		t.Fatalf("warning should mention the offending path, got logs: %q", logs)
	}
}

// TestValidate_InitialAllowedPaths_WorkflowInitialConflictDifferentModes verifies that
// when the same path is declared at both workflow-level and initial-level with
// different modes, the workflow loads successfully (initial-level mode is used)
// and a warning is logged.
func TestValidate_InitialAllowedPaths_WorkflowInitialConflictDifferentModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {
			"prompt": "do the thing",
			"allowed_paths": [
				{"path": "/srv/datasets", "mode": "read_write"}
			]
		},
		"allowed_paths": [
			{"path": "/srv/datasets", "mode": "read_only"}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the lock for the entire test body so the buffer snapshot is stable.
	logCaptureMu.Lock()
	defer logCaptureMu.Unlock()
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success with conflicting modes; got: %v", err)
	}
	// Workflow-level path is preserved.
	if len(cfg.AllowedPaths) != 1 {
		t.Fatalf("expected 1 workflow-level allowed_path, got %d", len(cfg.AllowedPaths))
	}
	// Initial-level path is also preserved.
	if cfg.Initial == nil {
		t.Fatal("expected Initial to be non-nil")
	}
	if len(cfg.Initial.AllowedPaths) != 1 {
		t.Fatalf("expected 1 initial-level allowed_path, got %d", len(cfg.Initial.AllowedPaths))
	}
	// Check warning was logged.
	logs := buf.String()
	if !strings.Contains(logs, "WARNING") {
		t.Fatalf("expected a WARNING log line for the mode conflict, got logs: %q", logs)
	}
	if !strings.Contains(logs, "/srv/datasets") {
		t.Fatalf("warning should mention the conflicting path, got logs: %q", logs)
	}
	if !strings.Contains(logs, "initial") {
		t.Fatalf("warning should mention 'initial', got logs: %q", logs)
	}
	if !strings.Contains(logs, "workflow level") {
		t.Fatalf("warning should mention 'workflow level', got logs: %q", logs)
	}
}

// TestValidate_StepAllowedPaths_MultipleSteps verifies that each step can have
// its own allowed_paths entries and validation is applied to all of them.
func TestValidate_StepAllowedPaths_MultipleSteps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.json")
	content := `{
		"initial": {"prompt": "do the thing"},
		"steps": [
			{
				"prompt": "step 1",
				"allowed_paths": [
					{"path": "/tmp/step1", "mode": "read_write"}
				]
			},
			{
				"prompt": "step 2",
				"allowed_paths": [
					{"path": "/tmp/step2", "mode": "read_only"}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("expected load success; got: %v", err)
	}
	if len(cfg.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(cfg.Steps))
	}
	if len(cfg.Steps[0].AllowedPaths) != 1 || cfg.Steps[0].AllowedPaths[0].Path != "/tmp/step1" {
		t.Errorf("step 0 allowed_paths wrong: %+v", cfg.Steps[0].AllowedPaths)
	}
	if len(cfg.Steps[1].AllowedPaths) != 1 || cfg.Steps[1].AllowedPaths[0].Path != "/tmp/step2" {
		t.Errorf("step 1 allowed_paths wrong: %+v", cfg.Steps[1].AllowedPaths)
	}
}