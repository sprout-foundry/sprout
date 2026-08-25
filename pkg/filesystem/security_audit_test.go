package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Filesystem audit logging (SP-127 Phase 2.6)
// ---------------------------------------------------------------------------

func TestSafeResolvePathWithBypass_AuditLoggerAllowed(t *testing.T) {
	// Create a test workspace in /var/tmp (which is not /tmp, so audit is emitted)
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)
	testFile := filepath.Join(workspace, "test.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithAuditLogger(ctx, logger)

	// Resolve path within workspace (should be allowed)
	result, err := SafeResolvePathWithBypass(ctx, testFile)
	if err != nil {
		t.Fatalf("SafeResolvePathWithBypass failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for allowed read, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "filesystem_read" {
		t.Errorf("Tool = %q, want 'filesystem_read'", e.Tool)
	}
	if e.Action != "allowed" {
		t.Errorf("Action = %q, want 'allowed'", e.Action)
	}
	if e.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want 'low'", e.RiskLevel)
	}
	if e.Category != "fs_gate" {
		t.Errorf("Category = %q, want 'fs_gate'", e.Category)
	}
}

func TestSafeResolvePathWithBypass_AuditLoggerDenied(t *testing.T) {
	// Create a test workspace
	workspace, _ := os.MkdirTemp("", "fs-audit-workspace-*")
	defer os.RemoveAll(workspace)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithAuditLogger(ctx, logger)

	// Try to resolve path outside workspace (should be denied)
	_, err := SafeResolvePathWithBypass(ctx, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for denied read, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "filesystem_read" {
		t.Errorf("Tool = %q, want 'filesystem_read'", e.Tool)
	}
	if e.Action != "denied" {
		t.Errorf("Action = %q, want 'denied'", e.Action)
	}
	if e.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want 'high'", e.RiskLevel)
	}
	if e.Category != "fs_gate" {
		t.Errorf("Category = %q, want 'fs_gate'", e.Category)
	}
}

func TestSafeResolvePathWithBypass_AuditLoggerAllowedViaSessionFolder(t *testing.T) {
	// Create a test workspace and allowed folder in /var/tmp
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)
	allowedFolder := filepath.Join("/var/tmp", "fs-audit-allowed-"+t.Name())
	os.MkdirAll(allowedFolder, 0755)
	defer os.RemoveAll(allowedFolder)

	// Create a test file in allowed folder
	testFile := filepath.Join(allowedFolder, "test.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with workspace and session allowlist
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithSessionAllowedFolders(ctx, []string{allowedFolder})
	ctx = WithAuditLogger(ctx, logger)

	// Resolve path in allowed folder (should be allowed)
	result, err := SafeResolvePathWithBypass(ctx, testFile)
	if err != nil {
		t.Fatalf("SafeResolvePathWithBypass failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for allowed read via session folder, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Action != "allowed" {
		t.Errorf("Action = %q, want 'allowed'", e.Action)
	}
}

func TestSafeResolvePathWithBypass_NilLoggerNoPanic(t *testing.T) {
	t.Parallel()

	// Create a test workspace in /var/tmp
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)

	// Build context with nil audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)

	// Should not panic
	_, err := SafeResolvePathWithBypass(ctx, "/etc/passwd")
	if err == nil {
		t.Error("expected error for path outside workspace")
	}
}

func TestSafeResolvePathForWriteWithBypass_AuditLoggerAllowed(t *testing.T) {
	// Create a test workspace in /var/tmp
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithAuditLogger(ctx, logger)

	// Resolve write path within workspace (should be allowed)
	result, err := SafeResolvePathForWriteWithBypass(ctx, filepath.Join(workspace, "newfile.txt"))
	if err != nil {
		t.Fatalf("SafeResolvePathForWriteWithBypass failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for allowed write, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "filesystem_write" {
		t.Errorf("Tool = %q, want 'filesystem_write'", e.Tool)
	}
	if e.Action != "allowed" {
		t.Errorf("Action = %q, want 'allowed'", e.Action)
	}
	if e.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want 'low'", e.RiskLevel)
	}
}

func TestSafeResolvePathForWriteWithBypass_AuditLoggerDenied(t *testing.T) {
	// Create a test workspace
	workspace, _ := os.MkdirTemp("", "fs-audit-workspace-*")
	defer os.RemoveAll(workspace)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithAuditLogger(ctx, logger)

	// Try to resolve write path outside workspace (should be denied)
	_, err := SafeResolvePathForWriteWithBypass(ctx, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for write path outside workspace")
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for denied write, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "filesystem_write" {
		t.Errorf("Tool = %q, want 'filesystem_write'", e.Tool)
	}
	if e.Action != "denied" {
		t.Errorf("Action = %q, want 'denied'", e.Action)
	}
	if e.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want 'high'", e.RiskLevel)
	}
}

func TestSafeResolvePathForWriteWithBypass_AuditLoggerSymlinkRedirected(t *testing.T) {
	// Create a test workspace, allowed folder, and symlink in /var/tmp
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)
	allowedFolder := filepath.Join("/var/tmp", "fs-audit-allowed-"+t.Name())
	os.MkdirAll(allowedFolder, 0755)
	defer os.RemoveAll(allowedFolder)

	// Create a file in the allowed folder (for the symlink to point to)
	existingFile := filepath.Join(allowedFolder, "existing.txt")
	os.WriteFile(existingFile, []byte("existing content"), 0644)

	// Create a symlink in workspace pointing to the file in allowed folder
	symlinkPath := existingFile + ".symlink"
	os.Symlink(existingFile, symlinkPath)

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with workspace and session allowlist
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)
	ctx = WithSessionAllowedFolders(ctx, []string{allowedFolder})
	ctx = WithAuditLogger(ctx, logger)

	// Resolve path through symlink (file exists, so symlink re-validation runs)
	result, err := SafeResolvePathWithBypass(ctx, symlinkPath)
	if err != nil {
		t.Fatalf("SafeResolvePathWithBypass failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result for symlink redirect")
	}

	// Should have an audit entry for the allowed symlink access
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for symlink redirect read, got %d: %v", len(entries), entries)
	}
	e := entries[0]
	if e.Tool != "filesystem_read" {
		t.Errorf("Tool = %q, want 'filesystem_read'", e.Tool)
	}
	if e.Action != "allowed" {
		t.Errorf("Action = %q, want 'allowed' (symlink was redirected but allowed via session folder)", e.Action)
	}
	if e.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want 'low'", e.RiskLevel)
	}
}

func TestSafeResolvePathForWriteWithBypass_NilLoggerNoPanic(t *testing.T) {
	t.Parallel()

	// Create a test workspace in /var/tmp
	workspace := filepath.Join("/var/tmp", "fs-audit-workspace-"+t.Name())
	os.MkdirAll(workspace, 0755)
	defer os.RemoveAll(workspace)

	// Build context with nil audit logger
	ctx := context.Background()
	ctx = WithWorkspaceRoot(ctx, workspace)

	// Should not panic
	_, err := SafeResolvePathForWriteWithBypass(ctx, "/etc/passwd")
	if err == nil {
		t.Error("expected error for write path outside workspace")
	}
}

func TestSafeResolvePath_TmpPathNoAudit(t *testing.T) {
	// On macOS, os.TempDir() returns /var/folders/... which is a symlink
	// to /private/var/folders/... and /var is in systemPathPrefixes. This
	// test is only valid on platforms where os.TempDir() is NOT under a
	// sensitive path prefix.
	if dir := os.TempDir(); strings.HasPrefix(filepath.Clean(dir), "/var") {
		t.Skip("os.TempDir() is under /var on macOS — classified as Sensitive")
	}

	// Create a simple audit logger wrapper that captures entries
	var entries []AuditEntry
	logger := &capturingAuditLogger{entries: &entries}

	// Build context with audit logger
	ctx := context.Background()
	ctx = WithAuditLogger(ctx, logger)

	// Create a temp file first so /tmp path exists
	tmpFile := filepath.Join(os.TempDir(), "testfile_audit.txt")
	os.WriteFile(tmpFile, []byte("test"), 0644)
	defer os.Remove(tmpFile)

	// Resolve path in /tmp (should not trigger audit per design - /tmp is always allowed)
	result, err := SafeResolvePathWithBypass(ctx, tmpFile)
	if err != nil {
		t.Fatalf("SafeResolvePathWithBypass failed for /tmp: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result for /tmp path")
	}

	// /tmp paths should not emit audit entries by design
	if len(entries) != 0 {
		t.Fatalf("expected 0 audit entries for /tmp path, got %d: %v", len(entries), entries)
	}
}

func TestWithAuditLogger_NilContext(t *testing.T) {
	// Should not panic
	ctx := WithAuditLogger(nil, nil)
	if ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestAuditLoggerFromContext_NilContext(t *testing.T) {
	logger := AuditLoggerFromContext(nil)
	if logger != nil {
		t.Error("expected nil logger for nil context")
	}
}

func TestAuditLoggerFromContext_NoLogger(t *testing.T) {
	ctx := context.Background()
	logger := AuditLoggerFromContext(ctx)
	if logger != nil {
		t.Error("expected nil logger when none was set")
	}
}

// capturingAuditLogger is a test double that captures entries in memory
// instead of writing them to disk. This is used to avoid importing agent_tools
// in test files and to simplify test assertions.
type capturingAuditLogger struct {
	entries *[]AuditEntry
}

func (l *capturingAuditLogger) LogEntry(entry any) error {
	if ae, ok := entry.(AuditEntry); ok {
		*l.entries = append(*l.entries, ae)
	}
	return nil
}

func (l *capturingAuditLogger) LogJSON(data []byte) error {
	var ae AuditEntry
	if err := json.Unmarshal(data, &ae); err != nil {
		return err
	}
	*l.entries = append(*l.entries, ae)
	return nil
}
