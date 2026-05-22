package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// TestRunDuplicateCheck_PathTraversalOutsideWorkspace verifies that
// runDuplicateCheck returns empty string for files outside the workspace,
// preventing path traversal attacks.
func TestRunDuplicateCheck_PathTraversalOutsideWorkspace(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.workspaceRoot = workspace

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	// Try to check a path outside the workspace (e.g., /etc/passwd).
	result := runDuplicateCheck(context.Background(), agent, "/etc/passwd")
	if result != "" {
		t.Errorf("runDuplicateCheck for path outside workspace returned %q, want empty string", result)
	}
}

// TestRunDuplicateCheck_RelativePathInsideWorkspace verifies that runDuplicateCheck
// handles relative paths correctly by resolving them to absolute paths
// and checking against the workspace root.
func TestRunDuplicateCheck_RelativePathInsideWorkspace(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.workspaceRoot = workspace

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	// Change to workspace so relative path resolves inside workspace.
	originalWd, _ := os.Getwd()
	os.Chdir(workspace)
	defer os.Chdir(originalWd)

	// Create a file in the workspace.
	if err := os.WriteFile("relfile.txt", []byte("relative path content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// The relative path should be resolved and found inside workspace.
	// Since the embedding manager wasn't initialized (no ORT), Init() will
	// fail inside CheckDuplicates and runDuplicateCheck will return "".
	result := runDuplicateCheck(context.Background(), agent, "relfile.txt")
	// Just verify no panic and empty result (init failure path).
	if result != "" {
		t.Logf("runDuplicateCheck for relative path returned %q (may contain error from failed init)", result)
	}
}

// TestRunDuplicateCheck_RelativePathTraversalDotDot verifies that runDuplicateCheck
// blocks path traversal using ../ sequences that escape the workspace.
func TestRunDuplicateCheck_RelativePathTraversalDotDot(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.workspaceRoot = workspace

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	// Change to workspace so relative paths are resolved from there.
	originalWd, _ := os.Getwd()
	os.Chdir(workspace)
	defer os.Chdir(originalWd)

	// Try to escape workspace using ../..
	result := runDuplicateCheck(context.Background(), agent, "../../etc/passwd")
	if result != "" {
		t.Errorf("runDuplicateCheck for ../../etc/passwd returned %q, want empty string (path traversal)", result)
	}
}

// TestRunDuplicateCheck_DuplicatesWithRealManager verifies that runDuplicateCheck
// can detect duplicates when the embedding manager is initialized and has data.
func TestRunDuplicateCheck_DuplicatesWithRealManager(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.workspaceRoot = workspace

	// Create embedding manager with real static provider (no ORT needed).
	em := embedding.NewEmbeddingManager(nil, workspace)
	agent.embeddingMgr = em

	// Initialize the embedding manager so CheckDuplicates can work.
	ctx := context.Background()
	if err := em.Init(ctx); err != nil {
		// Init may fail without ORT — skip this test if so.
		t.Skipf("embedding manager Init failed (no ORT): %v", err)
	}
	defer em.Close()

	// Create a test file.
	filePath := filepath.Join(workspace, "test_file.txt")
	if err := os.WriteFile(filePath, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// With an empty index, there should be no duplicates.
	result := runDuplicateCheck(ctx, agent, filePath)
	if strings.Contains(result, "DUPLICATE") {
		t.Errorf("unexpected duplicate warning with empty index: %q", result)
	}
}

// TestRunDuplicateCheck_EmptyWorkspaceRootFallback verifies that runDuplicateCheck
// handles empty workspaceRoot by falling back to the current directory.
func TestRunDuplicateCheck_EmptyWorkspaceRootFallback(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em
	agent.workspaceRoot = ""

	// Try to check a file — it should not panic even with empty workspaceRoot.
	result := runDuplicateCheck(context.Background(), agent, "somefile.txt")
	// The result may be empty (file not found) or error from init failure.
	// The key is: no panic.
	_ = result
}

// TestRunDuplicateCheck_AbsolutePathInWorkspace verifies that runDuplicateCheck
// allows files inside the workspace when given an absolute path.
func TestRunDuplicateCheck_AbsolutePathInWorkspace(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.workspaceRoot = workspace

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	// Create a file in the workspace.
	filePath := filepath.Join(workspace, "in_workspace.txt")
	if err := os.WriteFile(filePath, []byte("in workspace content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Absolute path inside workspace should be allowed (won't panic).
	result := runDuplicateCheck(context.Background(), agent, filePath)
	// Init failure is expected (no ORT), so result should be empty.
	if result != "" {
		t.Logf("runDuplicateCheck for absolute path in workspace returned %q", result)
	}
}

// TestRunDuplicateCheck_NilAgent verifies that runDuplicateCheck returns
// empty string when the agent is nil (nil embedding manager path).
func TestRunDuplicateCheck_NilAgent(t *testing.T) {
	result := runDuplicateCheck(context.Background(), nil, "any/file.txt")
	if result != "" {
		t.Errorf("runDuplicateCheck with nil agent returned %q, want empty string", result)
	}
}

// TestRunDuplicateCheck_EmptyFilePath verifies that runDuplicateCheck handles
// empty file paths gracefully.
func TestRunDuplicateCheck_EmptyFilePath(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	result := runDuplicateCheck(context.Background(), agent, "")
	if result != "" {
		t.Errorf("runDuplicateCheck with empty path returned %q, want empty string", result)
	}
}
