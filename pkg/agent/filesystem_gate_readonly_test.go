package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestFilesystemGateAdapter_ReadOnlyBlocksWrite is the SP-128-1f
// regression test: a write tool against a path under a session-
// allowlisted folder whose declared mode is read_only must be
// refused by the filesystem gate. The gate returns (ctx, false)
// AND attaches a denial reason so withFilesystemApproval surfaces
// the workflow-specific message rather than the generic
// ErrWriteOutsideWorkingDirectory sentinel.
//
// This test drives the contract from outside (adapter
// RequestPathApproval) all the way through to the error the
// withFilesystemApproval helper returns, so a regression at any
// layer of the chain is caught here.
//
// Not t.Parallel: newIsolatedTestAgent uses t.Setenv which is
// incompatible with parallel tests.
func TestFilesystemGateAdapter_ReadOnlyBlocksWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Use a tempdir under $HOME (matches the External tier
	// convention used elsewhere — Sensitive tier paths always
	// prompt and we need External to reach the
	// IsFolderSessionAllowed short-circuit). The legacy test
	// agent has no active WebUI client and no interactive
	// logger, so without an allowlist the gate would return
	// (ctx, false) without firing a prompt. Adding the folder
	// to the allowlist with mode=read_only routes the request
	// through the new write-block branch.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-sp128-readonly-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	a.AddSessionAllowedFolder(dir)
	a.SetSessionAllowedFolderMode(dir, "read_only")

	adapter := newFilesystemGateAdapter(a)
	if adapter == nil {
		t.Fatal("adapter must not be nil for a real agent")
	}

	ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", filepath.Join(dir, "out.txt"), "", filesystem.ErrWriteOutsideWorkingDirectory)
	if approved {
		t.Fatal("read_only allowed folder must NOT approve a write tool")
	}
	if filesystem.SecurityBypassEnabled(ctx) {
		t.Error("denial must NOT set the security bypass token")
	}

	// Denial reason must be retrievable from the context so the
	// withFilesystemApproval helper can surface the workflow-
	// specific message instead of the generic sentinel. The
	// helper lives in pkg/agent_tools; the test fetches it via
	// the test-only exported alias.
	reason := tools.DenialReasonForTest(ctx)
	if reason == "" {
		t.Fatal("gate must set a denial reason on the returned ctx")
	}
	if !strings.Contains(reason, "write blocked") {
		t.Errorf("denial reason should start with 'write blocked', got: %q", reason)
	}
	if !strings.Contains(reason, dir) {
		t.Errorf("denial reason should mention the offending path %q, got: %q", dir, reason)
	}
	if !strings.Contains(reason, "read_only") {
		t.Errorf("denial reason should mention 'read_only', got: %q", reason)
	}
}

// TestFilesystemGateAdapter_ReadOnlyAllowsRead is the symmetric
// check: a read tool against the same folder must still be
// approved. read_only constrains writes only; reads are unaffected
// (matches the IsFolderSessionAllowed / IsFolderSessionWriteAllowed
// split in the security submanager).
//
// Not t.Parallel: newIsolatedTestAgent uses t.Setenv.
func TestFilesystemGateAdapter_ReadOnlyAllowsRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-sp128-readonly-read-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	a.AddSessionAllowedFolder(dir)
	a.SetSessionAllowedFolderMode(dir, "read_only")

	adapter := newFilesystemGateAdapter(a)
	ctx, approved := adapter.RequestPathApproval(context.Background(), "read_file", filepath.Join(dir, "data.txt"), "", filesystem.ErrOutsideWorkingDirectory)
	if !approved {
		t.Fatal("read_only mode must NOT block read_file (mode is a write gate, not a general access toggle)")
	}
	if !filesystem.SecurityBypassEnabled(ctx) {
		t.Error("approved read should set the security bypass token")
	}
}

// TestFilesystemGateAdapter_ReadWriteAllowsWrite is the inverse of
// the read_only test: a read_write declaration must continue to
// approve writes so existing workflows (and the legacy
// AddSessionAllowedFolder callers) don't suddenly lose write
// access after upgrading.
//
// Not t.Parallel: newIsolatedTestAgent uses t.Setenv.
func TestFilesystemGateAdapter_ReadWriteAllowsWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-sp128-readwrite-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	a.AddSessionAllowedFolder(dir)
	a.SetSessionAllowedFolderMode(dir, "read_write")

	adapter := newFilesystemGateAdapter(a)
	ctx, approved := adapter.RequestPathApproval(context.Background(), "write_file", filepath.Join(dir, "out.txt"), "", filesystem.ErrWriteOutsideWorkingDirectory)
	if !approved {
		t.Fatal("read_write declaration must approve writes (default behavior)")
	}
	if !filesystem.SecurityBypassEnabled(ctx) {
		t.Error("approved write should set the security bypass token")
	}
}